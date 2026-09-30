import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { prepareLocalActionRetry, releaseLocalActionRetryAttempt, listLocalActionRetryEntries } from "../../lib/local-action-retry";
import { useConsoleBatchOwnership } from "../../connectors/templates/_shared/use-console-batch-ownership";
import { mutationTestWorkspace as workspace, mutationObservationQueue, setupMutationRetryStorage } from "../connector-mutation-test-state";

const path = "/api/console/bulk-exec";
const body = { target_ids: [7], command: "example", reason: "owner discovery", confirmation: "RUN ON 1 TARGETS" };
const accepted = { parallelism: 3, items: [{ request_id: 41, target_id: 7, target_name: "Example", status: "running" }] };
const queue = mutationObservationQueue();
setupMutationRetryStorage();
beforeEach(async () => {
  queue.clear();
  vi.stubGlobal("fetch", async () => json({}));
  await apiGet("/api/status");
});

it("starts locked until discovery succeeds and never performs a mutation during discovery", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const { result } = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
  expect(result.current.locked).toBe(true);
  await waitFor(() => expect(result.current.locked).toBe(false));
  expect(fetch).not.toHaveBeenCalled();
});

it("recovers accepted batch protection on remount and settles only the exact verified command", async () => {
  vi.stubGlobal("fetch", async () => json(accepted));
  await apiPost(path, body);
  const fetch = vi.fn(async (_url: string, _init: RequestInit) => json({ id: 41, runtime_id: 7, status: "running" }));
  vi.stubGlobal("fetch", fetch);
  const first = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
  await waitFor(() => expect(fetch).toHaveBeenCalledOnce());
  await waitFor(() => expect(queue.pending).toBe(1));
  expect(first.result.current.locked).toBe(true);
  first.unmount();
  const next = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  await waitFor(() => expect(queue.pending).toBe(1));
  fetch.mockImplementation(async () => json({ id: 41, runtime_id: 7, status: "completed" }));
  await queue.advance();
  expect(next.result.current.locked).toBe(false);
  expect(await listLocalActionRetryEntries()).toEqual([]);
  for (const [, init] of fetch.mock.calls) {
    expect(init.headers).toMatchObject({ "X-AIPermission-Workspace": workspace });
    expect(init.signal).toBeInstanceOf(AbortSignal);
  }
});

it("retains reply-loss identity with no request ID across close and reopen without POST", async () => {
  const retry = await prepareLocalActionRetry({ path, body }, { workspaceID: workspace, exclusiveConsoleBatch: true });
  await releaseLocalActionRetryAttempt(retry);
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  const view = renderHook(({ active }) => useConsoleBatchOwnership(active, true, queue.schedule), { initialProps: { active: true } });
  await waitFor(() => expect(queue.pending).toBe(1));
  view.rerender({ active: false });
  expect(queue.pending).toBe(0);
  view.rerender({ active: true });
  await waitFor(() => expect(queue.pending).toBe(1));
  expect(view.result.current.locked).toBe(true);
  expect(fetch).not.toHaveBeenCalled();
});

it.each(["outcome_unknown", "untracked"])(
  "retains %s outcomes for explicit reconciliation without automatic GET or POST",
  async (status) => {
    vi.stubGlobal("fetch", async () => json({ ...accepted, items: [{ ...accepted.items[0], status }] }));
    await apiPost(path, body);
    const fetch = vi.fn();
    vi.stubGlobal("fetch", fetch);
    const view = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
    await waitFor(() => expect(queue.pending).toBe(1));
    await queue.advance();
    expect(view.result.current.locked).toBe(true);
    expect(await listLocalActionRetryEntries()).toHaveLength(1);
    expect(fetch).not.toHaveBeenCalled();
  },
);

it.each(["rejected", "wrong-workspace", "wrong-runtime", "invalid-status"])(
  "retains protection after %s observation and recovers through a later GET",
  async (mode) => {
    vi.stubGlobal("fetch", async () => json(accepted));
    await apiPost(path, body);
    const fetch = vi.fn(async () => {
      if (mode === "rejected") throw new Error("observation failed");
      return json(
        { id: 41, runtime_id: mode === "wrong-runtime" ? 9 : 7, status: mode === "invalid-status" ? "future" : "completed" },
        mode === "wrong-workspace" ? "other" : workspace,
      );
    });
    vi.stubGlobal("fetch", fetch);
    const view = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
    await waitFor(() => expect(queue.pending).toBe(1));
    expect(view.result.current.locked).toBe(true);
    expect(await listLocalActionRetryEntries()).toHaveLength(1);
    fetch.mockImplementation(async () => json({ id: 41, runtime_id: 7, status: "completed" }));
    await queue.advance();
    expect(view.result.current.locked).toBe(false);
  },
);

it("aborts observations on unmount and ignores late results", async () => {
  vi.stubGlobal("fetch", async () => json(accepted));
  await apiPost(path, body);
  let finish!: (_response: Response) => void;
  let signal: AbortSignal | undefined;
  vi.stubGlobal("fetch", async (_url: string, init: RequestInit) => {
    signal = init.signal || undefined;
    return new Promise<Response>((resolve) => {
      finish = resolve;
    });
  });
  const view = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  view.unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    finish(json({ id: 41, runtime_id: 7, status: "running" }));
  });
  expect(queue.pending).toBe(0);
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it("drains all reads before scheduling another observation when one sibling rejects early", async () => {
  vi.stubGlobal("fetch", async () =>
    json({ ...accepted, items: [...accepted.items, { ...accepted.items[0], request_id: 42, target_id: 8 }] }),
  );
  await apiPost(path, { ...body, target_ids: [7, 8] });
  let finish!: (_response: Response) => void;
  const fetch = vi.fn(async (url: string) => {
    if (url.endsWith("/41")) throw new Error("Failed first sibling");
    return new Promise<Response>((resolve) => {
      finish = resolve;
    });
  });
  vi.stubGlobal("fetch", fetch);
  const view = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
  await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
  expect(queue.pending).toBe(0);
  expect(view.result.current.locked).toBe(true);
  await act(async () => finish(json({ id: 42, runtime_id: 8, status: "running" })));
  await waitFor(() => expect(queue.pending).toBe(1));
  expect(view.result.current.locked).toBe(true);
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("isolates a newly selected workspace from a late observation in the previous workspace", async () => {
  vi.stubGlobal("fetch", async () => json(accepted));
  await apiPost(path, body);
  let finish!: (_response: Response) => void;
  let signal: AbortSignal | null | undefined;
  vi.stubGlobal("fetch", async (url: string, init: RequestInit) => {
    if (url.endsWith("/api/status")) return json({}, "new-workspace");
    signal = init.signal;
    return new Promise<Response>((resolve) => {
      finish = resolve;
    });
  });
  const view = renderHook(() => useConsoleBatchOwnership(true, true, queue.schedule));
  await waitFor(() => expect(finish).toBeTypeOf("function"));
  await act(async () => {
    await apiGet("/api/status");
  });
  view.rerender();
  expect(signal?.aborted).toBe(true);
  await waitFor(() => expect(view.result.current.locked).toBe(false));
  await act(async () => finish(json({ id: 41, runtime_id: 7, status: "running" })));
  expect(view.result.current.workspaceID).toBe("new-workspace");
  expect(view.result.current.locked).toBe(false);
  expect(await listLocalActionRetryEntries("new-workspace")).toEqual([]);
  expect(await listLocalActionRetryEntries(workspace)).toHaveLength(1);
  await waitFor(() => expect(queue.pending).toBe(1));
});

function json(value: unknown, binding = workspace) {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json", "X-AIPermission-Workspace": binding, "X-AIPermission-Workspace-Changed": "true" },
  });
}
