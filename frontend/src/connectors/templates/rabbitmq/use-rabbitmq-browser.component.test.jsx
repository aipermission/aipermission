import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../../lib/api";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));
vi.mock("../../../lib/api", () => ({ apiGet: vi.fn() }));

const queues = [
  { name: "jobs.ready", vhost: "/", messages: 2 },
  { name: "jobs.failed", vhost: "/", messages: 1 },
];

beforeEach(() => {
  apiGet.mockReset();
  apiGet.mockResolvedValue([]);
  runGuardedConnectorAction.mockReset();
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input }) => responseFor(actionName, input));
});

function renderBrowser(approvals = { state: "ready", data: [] }) {
  return renderHook(
    ({ activity, targetRef = "rabbitmq:1:1" }) =>
      useRabbitMQBrowser({
        target: { ref: targetRef, config: { vhost: "/" } },
        approvals: activity,
        session: { active: true, startedAt: "now" },
        onRefreshActivity: vi.fn(),
      }),
    { initialProps: { activity: approvals, targetRef: "rabbitmq:1:1" } },
  );
}

it("loads and filters RabbitMQ queues through connector-owned state", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => result.current.setPattern("FAILED"));
  expect(result.current.filteredQueues.map((queue) => queue.name)).toEqual(["jobs.failed"]);
});

it("keeps the current queue list when a refresh returns no action item", async () => {
  runGuardedConnectorAction.mockResolvedValue(null);
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
  let resolveDetail;
  runGuardedConnectorAction.mockImplementation(async (options) => {
    if (options.actionName !== "get_queue") return responseFor(options.actionName, options.input);
    options.setState({ state: options.busy, error: "", message: "" });
    return new Promise((resolve) => {
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

  await act(async () => resolveDetail({ output: { name: "jobs.ready" } }));
  expect(result.current.queueDetail).toBeNull();
});

it("does not commit detail from a superseded RabbitMQ queue selection", async () => {
  const details = new Map();
  runGuardedConnectorAction.mockImplementation(({ actionName, input }) => {
    if (actionName !== "get_queue") return Promise.resolve(responseFor(actionName, input));
    return new Promise((resolve) => details.set(input.queue, resolve));
  });
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => void result.current.selectQueue("jobs.ready"));
  await waitFor(() => expect(details.has("jobs.ready")).toBe(true));
  act(() => void result.current.selectQueue("jobs.failed"));
  await waitFor(() => expect(details.has("jobs.failed")).toBe(true));

  await act(async () => details.get("jobs.ready")({ output: { name: "jobs.ready" } }));
  expect(result.current.queueDetail).toBeNull();
  await act(async () => details.get("jobs.failed")({ output: { name: "jobs.failed" } }));
  await waitFor(() => expect(result.current.queueDetail?.name).toBe("jobs.failed"));
});

it("rejects non-object publish properties before dispatch", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => {
    result.current.startPublish();
    result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "hello", properties: "[]" }));
  });
  runGuardedConnectorAction.mockClear();
  await act(async () => result.current.publishMessage());

  expect(result.current.state.error).toBe("Properties must be a JSON object.");
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();
});

it("does not dispatch queue reads while a vhost is only being edited", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  runGuardedConnectorAction.mockClear();

  act(() => result.current.setVhostDraft("/tenant"));

  expect(result.current.vhost).toBe("/");
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();

  act(() => result.current.applyVhost());
  await waitFor(() => expect(runGuardedConnectorAction).toHaveBeenCalledTimes(1));
  expect(runGuardedConnectorAction).toHaveBeenCalledWith(expect.objectContaining({ input: expect.objectContaining({ vhost: "/tenant" }) }));
});

it("keeps publish ownership when the vhost draft changes", async () => {
  let resolvePublish;
  runGuardedConnectorAction.mockImplementation((options) => {
    if (options.actionName !== "publish_message") return Promise.resolve(responseFor(options.actionName, options.input));
    options.setState({ state: options.busy, error: "", message: "" });
    return new Promise((resolve) => {
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
  await act(async () => resolvePublish({ output: {} }));

  await waitFor(() => expect(result.current.publish.payload).toBe(""));
});

it("keeps an approval-pending publish locked until activity becomes terminal", async () => {
  runGuardedConnectorAction.mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return responseFor(options.actionName, options.input);
    const pending = { request_id: 91, status: "approval_pending", display_text: "Awaiting approval" };
    options.onPending(pending);
    options.setState({ state: "idle", error: "", message: pending.display_text });
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
  const publishCalls = runGuardedConnectorAction.mock.calls.filter(([options]) => options.actionName === "publish_message");
  await act(async () => result.current.publishMessage());
  expect(runGuardedConnectorAction.mock.calls.filter(([options]) => options.actionName === "publish_message")).toHaveLength(
    publishCalls.length,
  );
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
  await waitFor(() => expect(result.current.publishLocked).toBe(false));
});

it("keeps an outcome-unknown publish locked for explicit reconciliation", async () => {
  runGuardedConnectorAction.mockImplementation(async (options) => {
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
  await waitFor(() => expect(result.current.publishLocked).toBe(false));
});

it("releases publish ownership after a definitive publish failure", async () => {
  runGuardedConnectorAction.mockImplementation(async (options) => {
    if (options.actionName !== "publish_message") return responseFor(options.actionName, options.input);
    throw new Error("publish rejected");
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
  let resolvePublish;
  runGuardedConnectorAction.mockImplementation((options) => {
    if (options.actionName !== "publish_message") return Promise.resolve(responseFor(options.actionName, options.input));
    return new Promise((resolve) => {
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
  const publishResolvers = [];
  runGuardedConnectorAction.mockImplementation((options) => {
    if (options.actionName !== "publish_message") return Promise.resolve(responseFor(options.actionName, options.input));
    return new Promise((resolve) => publishResolvers.push(resolve));
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

  await act(async () => publishResolvers[1]({ output: {} }));
  await waitFor(() => expect(result.current.publishLocked).toBe(false));
});

it("reconstructs pending publish ownership after remount", async () => {
  const pending = { id: 93, target_ref: "rabbitmq:1:1", action_name: "publish_message", status: "approval_pending" };
  const first = renderBrowser({ state: "ready", data: [pending] });
  await waitFor(() => expect(first.result.current.publishLocked).toBe(true));
  first.unmount();

  const second = renderBrowser({ state: "ready", data: [pending] });
  await waitFor(() => expect(second.result.current.publishLocked).toBe(true));
  runGuardedConnectorAction.mockClear();
  act(() => {
    second.result.current.setPublish((current) => ({ ...current, routingKey: "jobs.ready", payload: "duplicate" }));
  });
  await act(async () => second.result.current.publishMessage());
  expect(runGuardedConnectorAction).not.toHaveBeenCalled();
});

it("keeps pending publish ownership across structured session changes", async () => {
  const pending = { id: 94, target_ref: "rabbitmq:1:1", action_name: "publish_message", status: "approval_pending" };
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

function responseFor(actionName, input) {
  if (actionName === "list_queues") return { output: { queues } };
  if (actionName === "get_queue") return { output: { name: input.queue } };
  if (actionName === "list_bindings") return { output: { bindings: [] } };
  return { output: {} };
}
