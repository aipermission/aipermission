import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../../lib/api";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";
import type { RabbitBrowserProps } from "./browser-types";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { mutationTestWorkspace, setupMutationRetryStorage } from "../../../test/connector-mutation-test-state";
import { connectorApprovalFixture } from "../../../test/connector-action-fixtures";
import type { ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";
import { isDefinitiveConnectorActionStatus } from "../../../lib/gateway-contracts/connector-action-contract";

setupMutationRetryStorage();

vi.mock("../_shared/action-runner", async (importOriginal) => ({
  ...(await importOriginal<typeof import("../_shared/action-runner")>()),
  runGuardedConnectorAction: vi.fn(),
}));
vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), currentWorkspaceBinding: () => browserWorkspace }));
let browserWorkspace = mutationTestWorkspace;

const queues = [
  { name: "jobs.ready", vhost: "/", messages: 2 },
  { name: "jobs.failed", vhost: "/", messages: 1 },
];
const mockedGet = vi.mocked(apiGet);
const mockedRunner = vi.mocked(runGuardedConnectorAction);
const remoteRequests = new Map<number, ConnectorApproval>();
type ActionResolver = (_value: ConnectorActionResponse | null) => void;

function actionResponse(output: unknown, extra: Partial<ConnectorActionResponse> = {}): ConnectorActionResponse {
  return {
    status: "completed",
    request_id: 1,
    target_ref: "rabbitmq:1:1",
    connector_kind: "rabbitmq",
    action_name: "fixture",
    retry_policy: { class: "read_only", guidance: "Read again." },
    output,
    ...extra,
  };
}

beforeEach(() => {
  browserWorkspace = mutationTestWorkspace;
  mockedGet.mockReset();
  remoteRequests.clear();
  mockedGet.mockImplementation(async (path) => {
    const url = new URL(path, "http://localhost");
    const id = Number(url.pathname.split("/").at(-1));
    if (id) {
      const item = remoteRequests.get(id);
      if (!item) throw new Error("Synthetic request has not been observed");
      return item;
    }
    return [...remoteRequests.values()].filter(
      (item) =>
        item.target_ref === url.searchParams.get("target_ref") &&
        item.action_name === url.searchParams.get("action_name") &&
        !isDefinitiveConnectorActionStatus(item.status),
    );
  });
  mockedRunner.mockReset();
  mockedRunner.mockImplementation(async ({ actionName, input }) => responseFor(actionName, input));
});

function renderBrowser(approvals: NonNullable<RabbitBrowserProps["approvals"]> = { state: "ready", data: [] }) {
  const initialProps: { activity: NonNullable<RabbitBrowserProps["approvals"]>; targetRef?: string } = {
    activity: approvals,
    targetRef: "rabbitmq:1:1",
  };
  return renderHook(
    ({ activity, targetRef = "rabbitmq:1:1" }) => {
      recordRemoteActivity(activity);
      return useRabbitMQBrowser({
        target: { ref: targetRef, config: { vhost: "/" } },
        approvals: activity,
        session: { active: true, startedAt: "now" },
        onRefreshActivity: vi.fn(),
      });
    },
    { initialProps },
  );
}

it("does not clear a new workspace draft when an old workspace publish completes", async () => {
  let finish!: ActionResolver;
  mockedRunner.mockImplementation(async ({ actionName, input }) =>
    actionName === "publish_message"
      ? new Promise((resolve) => {
          finish = resolve;
        })
      : responseFor(actionName, input),
  );
  const rendered = renderBrowser();
  await waitFor(() => expect(rendered.result.current.publishLocked).toBe(false));
  act(() => rendered.result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "old workspace draft" })));
  act(() => {
    void rendered.result.current.publishMessage();
  });
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  browserWorkspace = "next-rabbit-workspace";
  rendered.rerender({ activity: { state: "ready", data: [] }, targetRef: "rabbitmq:1:1" });
  await waitFor(() => expect(rendered.result.current.publishLocked).toBe(false));
  act(() => rendered.result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "new workspace draft" })));
  await act(async () => {
    finish(actionResponse({ routed: true }));
  });
  expect(rendered.result.current.publish.payload).toBe("new workspace draft");
});

it("keeps malformed remote identities out of the queue workspace", async () => {
  mockedRunner.mockImplementation(async ({ actionName }) =>
    actionResponse(actionName === "list_queues" ? { queues: [null, { name: {} }, { name: "valid", state: [] }] } : { name: {} }),
  );
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(1));
  expect(result.current.queues[0].name).toBe("valid");
  expect(result.current.queues[0].state).toBeUndefined();
  await act(async () => result.current.selectQueue("valid"));
  expect(result.current.queueDetail).toBeNull();
  expect(result.current.bindings).toEqual([]);
});

it("loads and filters RabbitMQ queues through connector-owned state", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => result.current.setPattern("FAILED"));
  expect(result.current.filteredQueues.map((queue) => queue.name)).toEqual(["jobs.failed"]);
});

it("keeps the current queue list when a refresh returns no action item", async () => {
  mockedRunner.mockResolvedValue(null);
  const { result } = renderBrowser();

  await act(async () => result.current.refreshQueues());

  expect(result.current.queues).toEqual([]);
});

it("clears queue identity when the operator changes vhost", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  await act(async () => result.current.selectQueue("jobs.ready"));
  expect(result.current.queueDetail?.name).toBe("jobs.ready");

  act(() => result.current.setVhostDraft("/other"));
  act(() => result.current.applyVhost());
  expect(result.current.activeQueue).toBe("");
  expect(result.current.queueDetail).toBeNull();
  expect(result.current.bindings).toEqual([]);
});

it("keeps the selected queue when the vhost value does not change", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  await act(async () => result.current.selectQueue("jobs.ready"));
  expect(result.current.queueDetail?.name).toBe("jobs.ready");

  act(() => result.current.setVhostDraft("/"));
  act(() => result.current.applyVhost());

  expect(result.current.activeQueue).toBe("jobs.ready");
  expect(result.current.queueDetail?.name).toBe("jobs.ready");
});

it("retires busy queue reads when the operator changes vhost", async () => {
  let resolveDetail: ActionResolver = () => {};
  mockedRunner.mockImplementation(async (options) => {
    if (options.actionName !== "get_queue") return responseFor(options.actionName, options.input);
    options.setState({ state: options.busy || "running", error: "", message: "" });
    return new Promise<ConnectorActionResponse | null>((resolve) => {
      resolveDetail = resolve;
    });
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => void result.current.selectQueue("jobs.ready"));
  await waitFor(() => expect(result.current.state.state).toBe("reading"));

  act(() => result.current.setVhostDraft("/other"));
  act(() => result.current.applyVhost());
  expect(result.current.state).toEqual({ state: "idle", error: "", message: "" });

  await act(async () => resolveDetail(actionResponse({ name: "jobs.ready" })));
  expect(result.current.queueDetail).toBeNull();
});

it("does not commit detail from a superseded RabbitMQ queue selection", async () => {
  const details = new Map<string, ActionResolver>();
  mockedRunner.mockImplementation(({ actionName, input }) => {
    if (actionName !== "get_queue") return Promise.resolve(responseFor(actionName, input));
    return new Promise<ConnectorActionResponse | null>((resolve) => details.set(String(input?.queue), resolve));
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => void result.current.selectQueue("jobs.ready"));
  await waitFor(() => expect(details.has("jobs.ready")).toBe(true));
  act(() => void result.current.selectQueue("jobs.failed"));
  await waitFor(() => expect(details.has("jobs.failed")).toBe(true));

  await act(async () => details.get("jobs.ready")!(actionResponse({ name: "jobs.ready" })));
  expect(result.current.queueDetail).toBeNull();
  await act(async () => details.get("jobs.failed")!(actionResponse({ name: "jobs.failed" })));
  await waitFor(() => expect(result.current.queueDetail?.name).toBe("jobs.failed"));
});

it("rejects non-object publish properties before dispatch", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello", properties: "[]" }));
  });
  mockedRunner.mockClear();
  await act(async () => result.current.publishMessage());

  expect(result.current.state.error).toBe("Properties must be a JSON object.");
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();
});

it("does not dispatch queue reads while a vhost is only being edited", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  mockedRunner.mockClear();

  act(() => result.current.setVhostDraft("/tenant"));

  expect(result.current.vhost).toBe("/");
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();

  act(() => result.current.applyVhost());
  await waitFor(() => expect(runGuardedConnectorAction).toHaveBeenCalledTimes(1));
  expect(runGuardedConnectorAction).toHaveBeenCalledWith(expect.objectContaining({ input: expect.objectContaining({ vhost: "/tenant" }) }));
});

it("keeps publish ownership when the vhost draft changes", async () => {
  let resolvePublish: ActionResolver = () => {};
  mockedRunner.mockImplementation((options) => {
    if (options.actionName !== "publish_message") return Promise.resolve(responseFor(options.actionName, options.input));
    options.setState({ state: options.busy || "running", error: "", message: "" });
    return new Promise<ConnectorActionResponse | null>((resolve) => {
      resolvePublish = resolve;
    });
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello" }));
  });
  act(() => void result.current.publishMessage());
  await waitFor(() => expect(result.current.state.state).toBe("publishing"));

  act(() => result.current.setVhostDraft("/other"));
  expect(result.current.vhost).toBe("/");
  await act(async () => resolvePublish(actionResponse({})));

  await waitFor(() => expect(result.current.publish.payload).toBe(""));
});

it("keeps an approval-pending publish locked until activity becomes terminal", async () => {
  mockedRunner.mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return responseFor(options.actionName, options.input);
    const pending = actionResponse({}, { request_id: 91, status: "approval_pending", display_text: "Awaiting approval" });
    options.onPending?.(pending);
    options.setState({ state: "idle", error: "", message: pending.display_text || "" });
    return null;
  });
  const { result, rerender } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello" }));
  });
  await act(async () => result.current.publishMessage());

  expect(result.current.publishLocked).toBe(true);
  expect(result.current.publish.payload).toBe("hello");
  const publishCalls = mockedRunner.mock.calls.filter(([options]) => options.actionName === "publish_message");
  await act(async () => result.current.publishMessage());
  expect(mockedRunner.mock.calls.filter(([options]) => options.actionName === "publish_message")).toHaveLength(publishCalls.length);
  act(() => {
    result.current.setVhostDraft("/other");
    result.current.applyVhost();
    void result.current.selectQueue("jobs.failed");
  });
  expect(result.current.vhost).toBe("/");
  expect(result.current.activeQueue).not.toBe("jobs.failed");

  rerender({ activity: { state: "ready", data: [{ id: 91, target_ref: "rabbitmq:1:1", status: "approval_pending" }] } });
  await waitFor(() => expect(result.current.publishLocked).toBe(true));
  rerender({ activity: { state: "ready", data: [{ id: 91, target_ref: "rabbitmq:1:1", status: "completed" }] } });
  await waitFor(() => expect(result.current.publishLocked).toBe(false), { timeout: 4500 });
});

it("keeps an outcome-unknown publish locked for explicit reconciliation", async () => {
  mockedRunner.mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return responseFor(options.actionName, options.input);
    throw Object.assign(new Error("outcome unknown"), {
      data: { request_id: 92, status: "outcome_unknown" },
    });
  });
  const { result, rerender } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello" }));
  });
  await act(async () => result.current.publishMessage());

  expect(result.current.publishLocked).toBe(true);
  rerender({ activity: { state: "ready", data: [{ id: 92, target_ref: "rabbitmq:1:1", status: "outcome_unknown" }] } });
  await waitFor(() => expect(result.current.publishLocked).toBe(true));
  rerender({ activity: { state: "ready", data: [{ id: 92, target_ref: "rabbitmq:1:1", status: "failed" }] } });
  await waitFor(() => expect(result.current.publishLocked).toBe(false), { timeout: 4500 });
});

it("releases publish ownership after a definitive publish failure", async () => {
  mockedRunner.mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return responseFor(options.actionName, options.input);
    throw Object.assign(new Error("publish rejected"), {
      actionItem: actionResponse({}, { status: "failed", action_name: "publish_message", error: "publish rejected" }),
    });
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello" }));
  });

  await act(async () => result.current.publishMessage());

  expect(result.current.publishLocked).toBe(false);
});

it("releases provisional publish ownership when a target change retires the request", async () => {
  let resolvePublish: ActionResolver = () => {};
  mockedRunner.mockImplementation((options) => {
    if (options.actionName !== "publish_message") return Promise.resolve(responseFor(options.actionName, options.input));
    return new Promise<ConnectorActionResponse | null>((resolve) => {
      resolvePublish = resolve;
    });
  });
  const { result, rerender } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello" }));
  });
  act(() => void result.current.publishMessage());
  await waitFor(() => expect(result.current.publishLocked).toBe(true));

  rerender({ activity: { state: "ready", data: [] }, targetRef: "rabbitmq:2:2" });
  await act(async () => resolvePublish(null));

  await waitFor(() => expect(result.current.publishLocked).toBe(false));
});

it("does not let a stale publish continuation release a newer publish owner", async () => {
  const publishResolvers: ActionResolver[] = [];
  mockedRunner.mockImplementation((options) => {
    if (options.actionName !== "publish_message") return Promise.resolve(responseFor(options.actionName, options.input));
    return new Promise<ConnectorActionResponse | null>((resolve) => publishResolvers.push(resolve));
  });
  const { result, rerender } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "first" }));
  });
  act(() => void result.current.publishMessage());
  await waitFor(() => expect(result.current.publishLocked).toBe(true));

  rerender({ activity: { state: "ready", data: [] }, targetRef: "rabbitmq:2:2" });
  await waitFor(() => expect(result.current.publishLocked).toBe(false));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.next", payload: "second" }));
  });
  act(() => void result.current.publishMessage());
  await waitFor(() => expect(publishResolvers).toHaveLength(2));
  expect(result.current.publishLocked).toBe(true);

  await act(async () => publishResolvers[0](null));
  expect(result.current.publishLocked).toBe(true);
  expect(result.current.publish.payload).toBe("second");

  await act(async () => publishResolvers[1](actionResponse({})));
  await waitFor(() => expect(result.current.publishLocked).toBe(false));
});

it("reconstructs pending publish ownership after remount", async () => {
  const pending = { id: 93, target_ref: "rabbitmq:1:1", action_name: "publish_message", status: "approval_pending" };
  const first = renderBrowser({ state: "ready", data: [pending] });
  await waitFor(() => expect(first.result.current.publishLocked).toBe(true));
  first.unmount();

  const second = renderBrowser({ state: "ready", data: [pending] });
  await waitFor(() => expect(second.result.current.publishLocked).toBe(true));
  mockedRunner.mockClear();
  act(() => {
    second.result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "duplicate" }));
  });
  await act(async () => second.result.current.publishMessage());
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();
});

it("keeps pending publish ownership across structured session changes", async () => {
  const pending = { id: 94, target_ref: "rabbitmq:1:1", action_name: "publish_message", status: "approval_pending" };
  recordRemoteActivity({ state: "ready", data: [pending] });
  const { result, rerender } = renderHook(
    ({ startedAt }) =>
      useRabbitMQBrowser({
        target: { ref: "rabbitmq:1:1", config: { vhost: "/" } },
        approvals: { state: "ready", data: [pending] },
        session: { active: true, startedAt },
        onRefreshActivity: vi.fn(),
      }),
    { initialProps: { startedAt: "first" } },
  );
  await waitFor(() => expect(result.current.publishLocked).toBe(true));
  rerender({ startedAt: "second" });
  await waitFor(() => expect(result.current.publishLocked).toBe(true));
});

function recordRemoteActivity(activity: NonNullable<RabbitBrowserProps["approvals"]>) {
  for (const item of activity.data || []) {
    if (!item.id || !item.target_ref) continue;
    remoteRequests.set(
      item.id,
      connectorApprovalFixture({
        id: item.id,
        target_ref: item.target_ref,
        connector_kind: "rabbitmq",
        action_name: item.action_name || "publish_message",
        status: item.status as ConnectorApproval["status"],
      }),
    );
  }
}

function responseFor(actionName: string, input?: Record<string, unknown>) {
  const output =
    actionName === "list_queues"
      ? { queues }
      : actionName === "get_queue"
        ? { name: input?.queue }
        : actionName === "list_bindings"
          ? { bindings: [] }
          : {};
  return actionResponse(output, { action_name: actionName });
}
