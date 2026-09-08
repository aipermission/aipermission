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

  act(() => result.current.setVhost("/other"));
  expect(result.current.activeQueue).toBe("");
  expect(result.current.queueDetail).toBeNull();
  expect(result.current.bindings).toEqual([]);
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

function responseFor(actionName, input) {
  if (actionName === "list_queues") return { output: { queues } };
  if (actionName === "get_queue") return { output: { name: input.queue } };
  if (actionName === "list_bindings") return { output: { bindings: [] } };
  return { output: {} };
}
