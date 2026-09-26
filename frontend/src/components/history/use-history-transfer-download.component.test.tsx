import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDownload } from "../../lib/api";
import { useHistoryTransferDownload } from "./use-history-transfer-download";

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn() }));
beforeEach(() => { vi.mocked(apiDownload).mockReset(); });

it("does not dispatch a history download without a selected item", async () => {
  const { result } = renderHook(() => useHistoryTransferDownload(null, "file.txt"));
  await act(async () => result.current.downloadTransfer());
  expect(apiDownload).not.toHaveBeenCalled();
  expect(result.current.downloadState.state).toBe("idle");
});

it("releases the download state when the current file picker is canceled", async () => {
  vi.mocked(apiDownload).mockRejectedValueOnce(new DOMException("Canceled", "AbortError")).mockResolvedValueOnce({ saved: true, method: "picker" });
  const { result } = renderHook(() => useHistoryTransferDownload({ id: 1, source_ref_id: 7 }, "file.txt"));
  await act(async () => result.current.downloadTransfer());
  expect(result.current.downloadState).toEqual({ state: "idle", error: null });
  await act(async () => result.current.downloadTransfer());
  expect(apiDownload).toHaveBeenCalledTimes(2);
  expect(result.current.downloadState).toEqual({ state: "idle", error: null });
});

it("reports arbitrary current download failures and allows a successful retry", async () => {
  vi.mocked(apiDownload).mockRejectedValueOnce("stream unavailable").mockResolvedValueOnce({ saved: true, method: "picker" });
  const { result } = renderHook(() => useHistoryTransferDownload({ id: 1, source_ref_id: 7 }, "file.txt"));
  await act(async () => result.current.downloadTransfer());
  expect(result.current.downloadState).toEqual({ state: "error", error: "stream unavailable" });
  await act(async () => result.current.downloadTransfer());
  expect(result.current.downloadState).toEqual({ state: "idle", error: null });
  expect(apiDownload).toHaveBeenCalledWith("/api/file-transfers/7/download", "file.txt", expect.objectContaining({ requireStreaming: true, picker: true }));
});

it.each(["selection", "unmount", "replacement"])("aborts a history download after %s and ignores late failure", async (change) => {
  let reject: ((_error: Error) => void) | undefined;
  let finish: (() => void) | undefined;
  vi.mocked(apiDownload).mockImplementationOnce(() => new Promise((_resolve, rejectPromise) => { reject = rejectPromise; }))
    .mockImplementationOnce(() => new Promise((resolve) => { finish = () => resolve({ saved: true, method: "picker" }); }));
  const { result, rerender, unmount } = renderHook((item) => useHistoryTransferDownload(item, "file.txt"), { initialProps: { id: 1, source_ref_id: 7 } });
  let old: Promise<void> | undefined;
  act(() => { old = result.current.downloadTransfer(); });
  const options: unknown = vi.mocked(apiDownload).mock.calls[0]?.[2];
  if (!options || typeof options !== "object" || !("signal" in options) || !(options.signal instanceof AbortSignal)) throw new Error("Missing download cancellation signal");
  const signal = options.signal;
  let replacement: Promise<void> | undefined;
  if (change === "selection") rerender({ id: 2, source_ref_id: 8 });
  else if (change === "unmount") unmount();
  else act(() => { replacement = result.current.downloadTransfer(); });
  expect(signal.aborted).toBe(true);
  await act(async () => { reject?.(new Error("old failure")); await old; });
  if (change === "replacement") {
    expect(result.current.downloadState.state).toBe("downloading");
    await act(async () => { finish?.(); await replacement; });
    expect(result.current.downloadState.state).toBe("idle");
  } else if (change === "selection") expect(result.current.downloadState).toEqual({ state: "idle", error: null });
});
