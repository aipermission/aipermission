import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

const queues = [
  { name: "jobs.ready", vhost: "/", messages: 2 },
  { name: "jobs.failed", vhost: "/", messages: 1 },
];

beforeEach(() => {
  runGuardedConnectorAction.mockReset();
  runGuardedConnectorAction.mockImplementation(async ({ actionName, input }) => responseFor(actionName, input));
});

function renderBrowser() {
  return renderHook(() =>
    useRabbitMQBrowser({
      target: { ref: "rabbitmq:1:1", config: { vhost: "/" } },
      approvals: { data: [] },
      session: { active: true, startedAt: "now" },
      onRefreshActivity: vi.fn(),
    }),
  );
}

it("loads and filters RabbitMQ queues through connector-owned state", async () => {
  const { result } = renderBrowser();
  await waitFor(() => expect(result.current.queues).toHaveLength(2));
  act(() => result.current.setPattern("FAILED"));
  expect(result.current.filteredQueues.map((queue) => queue.name)).toEqual(["jobs.failed"]);
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

function responseFor(actionName, input) {
  if (actionName === "list_queues") return { output: { queues } };
  if (actionName === "get_queue") return { output: { name: input.queue } };
  if (actionName === "list_bindings") return { output: { bindings: [] } };
  return { output: {} };
}
