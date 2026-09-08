import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useTransferCenterState } from "./use-transfer-center-state";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((next) => {
    resolve = next;
  });
  return { promise, resolve };
}

describe("useTransferCenterState", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
  });

  it("opens once for a newly observed pending approval", async () => {
    apiGet.mockResolvedValue({ items: [{ id: 7, status: "pending_approval" }] });
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));

    await act(async () => result.current.loadBatches());
    expect(result.current.open).toBe(true);
    act(() => result.current.close());
    await act(async () => result.current.loadBatches());

    expect(result.current.open).toBe(false);
    expect(result.current.activeCount).toBe(1);
  });

  it("applies an action result before replacing it with the refreshed list", async () => {
    const post = deferred();
    apiPost.mockReturnValue(post.promise);
    let resolveRefresh;
    apiGet.mockReturnValue(
      new Promise((resolve) => {
        resolveRefresh = resolve;
      }),
    );
    const { result } = renderHook(() => useTransferCenterState({ pollIsCurrent: () => true }));

    let action;
    act(() => {
      action = result.current.actions.pause(4);
    });
    await act(async () => post.resolve({ id: 4, status: "paused" }));
    expect(result.current.batches.data).toEqual([{ id: 4, status: "paused" }]);

    await act(async () => resolveRefresh({ items: [{ id: 4, status: "running" }] }));
    await action;
    expect(result.current.batches.data).toEqual([{ id: 4, status: "running" }]);
  });
});
