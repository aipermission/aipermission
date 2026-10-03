import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../../lib/api";
import { connectorActionFixture, connectorActionRequest } from "../../../test/connector-action-fixtures";
import { mutationTestWorkspace, setupMutationRetryStorage } from "../../../test/connector-mutation-test-state";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { QueueBrowser } from "./queue-browser";
import { useRabbitMQBrowser } from "./use-rabbitmq-browser";

vi.mock("../../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn(), currentWorkspaceBinding: () => mutationTestWorkspace }));
setupMutationRetryStorage();
const firstPage = Array.from({ length: 250 }, (_, id) => ({ name: `jobs.${id}`, vhost: "/" }));
const props = {
  target: { ref: "rabbitmq:1:1", config: { vhost: "/" } },
  approvals: { state: "ready", data: [] },
  session: { active: true, startedAt: "fixture" },
};
function reply(payload: unknown, output: unknown) {
  const { action_name: actionName, target_ref: targetRef } = connectorActionRequest(payload);
  return connectorActionFixture({ connector_kind: "rabbitmq", target_ref: targetRef, action_name: actionName, output });
}
beforeEach(() => {
  vi.mocked(apiGet).mockReset().mockResolvedValue([]);
  vi.mocked(apiPost)
    .mockReset()
    .mockImplementation(async (_path, payload) => {
      const { action_name: actionName, target_ref: targetRef, input } = connectorActionRequest(payload);
      return connectorActionFixture({
        connector_kind: "rabbitmq",
        target_ref: targetRef,
        action_name: actionName,
        output:
          actionName === "list_queues"
            ? {
                queues: input.pattern === "jobs.final" ? [{ name: "jobs.final", vhost: "/" }] : firstPage,
                truncated: input.pattern !== "jobs.final",
                scan_limit_reached: false,
              }
            : actionName === "get_queue"
              ? { name: input.queue }
              : { bindings: [] },
      });
    });
});

it("finds and selects a queue beyond the first 250 through the real guarded action runner", async () => {
  const { result } = renderHook(() => useRabbitMQBrowser(props));
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  act(() => result.current.setPattern("jobs.final"));
  expect(result.current.filteredQueues).toEqual([]);
  await act(async () => result.current.refreshQueues());
  expect(result.current.filteredQueues.map((queue) => queue.name)).toEqual(["jobs.final"]);
  await act(async () => result.current.selectQueue("jobs.final"));
  expect(result.current.queueDetail?.name).toBe("jobs.final");
  expect(apiPost).toHaveBeenCalledWith(
    "/api/connector-actions/local-run",
    expect.objectContaining({ input: { vhost: "/", pattern: "jobs.final", limit: 250 } }),
    expect.anything(),
  );
});

it("submits server search with Enter and distinguishes local preview from a partial result", async () => {
  function Fixture() {
    return <QueueBrowser browser={useRabbitMQBrowser(props)} styles={connectorConsoleTheme("dark")} />;
  }
  render(<Fixture />);
  await screen.findByText(/Partial queue list/);
  const input = screen.getByPlaceholderText("Filter queues");
  fireEvent.change(input, { target: { value: "jobs.final" } });
  expect(screen.getByText(/No loaded queues match/)).toBeVisible();
  fireEvent.submit(input.closest("form")!);
  await screen.findByRole("button", { name: '"jobs.final"' });
  expect(screen.queryByText(/Partial queue list/)).not.toBeInTheDocument();
});

it("keeps the submitted filter distinct from edits while its response is pending", async () => {
  const { result } = renderHook(() => useRabbitMQBrowser(props));
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  let finish!: () => void;
  vi.mocked(apiPost).mockImplementationOnce(
    async (_path, payload) =>
      new Promise((resolve) => {
        finish = () => resolve(reply(payload, { queues: [], truncated: true, scan_limit_reached: true }));
      }),
  );
  act(() => result.current.setPattern("old"));
  act(() => {
    void result.current.refreshQueues();
  });
  act(() => result.current.setPattern("new"));
  await act(async () => finish());
  expect(result.current.queueDiscovery).toEqual({ appliedPattern: "old", partial: true, scanLimitReached: true });
  expect(result.current.pattern).toBe("new");
});

it("retires an earlier search even after the query returns to the same text", async () => {
  const { result } = renderHook(() => useRabbitMQBrowser(props));
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  let finish!: () => void;
  vi.mocked(apiPost).mockImplementationOnce(
    async (_path, payload) =>
      new Promise((resolve) => {
        finish = () => resolve(reply(payload, { queues: [{ name: "jobs.old.stale" }], truncated: true, scan_limit_reached: true }));
      }),
  );
  act(() => result.current.setPattern("jobs.old"));
  act(() => {
    void result.current.refreshQueues();
  });
  act(() => result.current.setPattern("jobs.final"));
  await act(async () => result.current.refreshQueues());
  vi.mocked(apiPost).mockImplementationOnce(async (_path, payload) =>
    reply(payload, { queues: [{ name: "jobs.old.current" }], truncated: false, scan_limit_reached: false }),
  );
  act(() => result.current.setPattern("jobs.old"));
  await act(async () => result.current.refreshQueues());
  await act(async () => finish());
  expect(result.current.queues.map((queue) => queue.name)).toEqual(["jobs.old.current"]);
  expect(result.current.queueDiscovery).toEqual({ appliedPattern: "jobs.old", partial: false, scanLimitReached: false });
});

it("clears the previous vhost window before loading and rejects its late ABA response", async () => {
  const { result } = renderHook(() => useRabbitMQBrowser(props));
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  let finish!: () => void;
  vi.mocked(apiPost).mockImplementationOnce(
    async (_path, payload) =>
      new Promise((resolve) => {
        finish = () => resolve(reply(payload, { queues: [{ name: "stale" }], truncated: true, scan_limit_reached: true }));
      }),
  );
  act(() => {
    void result.current.refreshQueues();
  });
  act(() => result.current.setVhostDraft("next"));
  act(() => result.current.applyVhost());
  expect(result.current.queues).toEqual([]);
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  act(() => result.current.setVhostDraft("/"));
  act(() => result.current.applyVhost());
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  await act(async () => finish());
  expect(result.current.queues).toEqual(firstPage);
  expect(result.current.queueDiscovery.scanLimitReached).toBe(false);
});

it.each([
  { output: { queues: [], truncated: true, scan_limit_reached: true }, partial: true, scan: true },
  { output: { queues: [], truncated: false, scan_limit_reached: false }, partial: false, scan: false },
  { output: { queues: [] }, partial: true, scan: false },
  { output: null, partial: true, scan: false },
])("preserves empty and incomplete discovery metadata: $output", async ({ output, partial, scan }) => {
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => reply(payload, output));
  function Fixture() {
    return <QueueBrowser browser={useRabbitMQBrowser(props)} styles={connectorConsoleTheme("dark")} />;
  }
  render(<Fixture />);
  expect(
    await screen.findByText(partial ? "No queues shown in this partial result." : "No queues found for this vhost/filter."),
  ).toBeVisible();
  expect(screen.queryByText(/Partial queue list/)).toEqual(partial ? expect.any(HTMLElement) : null);
  expect(screen.queryByText(/scan limit reached/)).toEqual(scan ? expect.any(HTMLElement) : null);
});

it("keeps a searched queue selected when bindings await approval or return no structured rows", async () => {
  const { result } = renderHook(() => useRabbitMQBrowser(props));
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => {
    const request = connectorActionRequest(payload);
    return reply(payload, request.action_name === "get_queue" ? { name: request.input.queue } : null);
  });
  await act(async () => result.current.selectQueue("jobs.0"));
  expect(result.current.bindings).toEqual([]);
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => {
    const request = connectorActionRequest(payload);
    const item = reply(payload, { name: request.input.queue });
    return request.action_name === "list_bindings" ? { ...item, status: "approval_pending", display_text: "Approval pending" } : item;
  });
  await act(async () => result.current.selectQueue("jobs.1"));
  expect(result.current.queueDetail?.name).toBe("jobs.1");
  expect(result.current.bindings).toEqual([]);
});

it("retains bounded peeking and selected-queue publishing defaults after server discovery", async () => {
  const { result } = renderHook(() => useRabbitMQBrowser(props));
  await waitFor(() => expect(result.current.queues).toHaveLength(250));
  await act(async () => result.current.selectQueue("jobs.0"));
  act(() => result.current.setPeekCount(0));
  vi.mocked(apiPost).mockImplementationOnce(async (_path, payload) => reply(payload, null));
  await act(async () => result.current.peekMessages());
  expect(result.current.messages).toEqual([]);
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-actions/local-run",
    expect.objectContaining({ input: { vhost: "/", queue: "jobs.0", count: 5, max_payload_bytes: 65536 } }),
    expect.anything(),
  );
  act(() => result.current.setPublish((current) => ({ ...current, routingKey: "" })));
  act(() => result.current.startPublish());
  expect(result.current.publish.routingKey).toBe("jobs.0");
});
