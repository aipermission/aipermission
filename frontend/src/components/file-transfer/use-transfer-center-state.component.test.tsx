import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useTransferCenterState } from "./use-transfer-center-state";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

function deferred() {
  let resolve: (_value: unknown) => void = () => {
    throw new Error("Uninitialized request");
  };
  const promise = new Promise<unknown>((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("useTransferCenterState", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPost).mockReset();
  });

  it("opens once for a newly observed pending approval", async () => {
    vi.mocked(apiGet).mockResolvedValue({ items: [{ id: 7, status: "pending_approval" }] });
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));

    await act(async () => result.current.loadBatches());
    expect(result.current.open).toBe(true);
    act(() => result.current.close());
    await act(async () => result.current.loadBatches());

    expect(result.current.open).toBe(false);
    expect(result.current.activeCount).toBe(1);
  });

  it("bounds poll reads without imposing a deadline on manual refresh", async () => {
    vi.mocked(apiGet).mockResolvedValue({ items: [] });
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));

    await act(async () => result.current.loadBatches({ keepData: true }, 2));
    await act(async () => result.current.loadBatches({ keepData: true }));

    expect(vi.mocked(apiGet).mock.calls[0][1]).toEqual({ signal: undefined, timeoutMs: 4000 });
    expect(vi.mocked(apiGet).mock.calls[1][1]).toEqual({ signal: undefined });
  });

  it("applies an action result before replacing it with the refreshed list", async () => {
    const post = deferred();
    vi.mocked(apiPost).mockReturnValue(post.promise);
    const refresh = deferred();
    vi.mocked(apiGet).mockReturnValue(refresh.promise);
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));

    let action: Promise<unknown> | undefined;
    act(() => {
      action = result.current.actions.pause(4);
    });
    await act(async () => post.resolve({ id: 4, status: "paused" }));
    expect(result.current.batches.data).toEqual([{ id: 4, status: "paused" }]);

    await act(async () => refresh.resolve({ items: [{ id: 4, status: "running" }] }));
    await action;
    expect(result.current.batches.data).toEqual([{ id: 4, status: "running" }]);
  });

  it("retains the loaded list on transient manual-refresh failures", async () => {
    vi.mocked(apiGet)
      .mockResolvedValueOnce({ items: [{ id: 7, status: "running" }] })
      .mockRejectedValueOnce("unavailable");
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));
    await act(async () => result.current.loadBatches());
    await act(async () => result.current.loadBatches({ keepData: true }));
    expect(result.current.batches).toEqual({ state: "error", error: "unavailable", data: [{ id: 7, status: "running" }] });
  });

  it("ignores late poll failures when the poll generation is stale", async () => {
    const isCurrent = vi.fn(() => true);
    vi.mocked(apiGet).mockRejectedValueOnce(new Error("old failure"));
    isCurrent.mockReturnValue(false);
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: isCurrent }));
    await act(async () => result.current.loadBatches({ keepData: true }, 3));
    expect(result.current.batches).toEqual({ state: "loading", data: [], error: null });
    expect(isCurrent).toHaveBeenCalledWith(3);
  });

  it.each([null, {}, { items: null }, { items: [{}] }, { items: [{ id: 0, status: "running" }] }, { items: [{ id: 7, status: false }] }])(
    "reports a malformed list without accepting unvalidated items",
    async (response) => {
      vi.mocked(apiGet).mockResolvedValueOnce(response);
      const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));
      await act(async () => result.current.loadBatches());
      expect(result.current.batches.state).toBe("error");
      expect(result.current.batches.error).toMatch(/Invalid transfer list/);
      expect(result.current.batches.data).toEqual([]);
    },
  );

  it("rejects an invalid action response before applying or refreshing", async () => {
    vi.mocked(apiPost).mockResolvedValueOnce({ id: "7", status: "paused" });
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));
    await act(async () => {
      await expect(result.current.actions.pause(7)).rejects.toThrow("Invalid transfer list batch");
    });
    expect(apiGet).not.toHaveBeenCalled();
    expect(result.current.batches.data).toEqual([]);
  });
});
