import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useVaultActionApprovals } from "./use-vault-action-approvals";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

function renderApprovals() {
  const refreshConsoleSessions = vi.fn().mockResolvedValue(undefined);
  const hook = renderHook(() => useVaultActionApprovals({ pollIsCurrent: () => true, refreshConsoleSessions }));
  return { ...hook, refreshConsoleSessions };
}

const pendingApproval = { id: 42, status: "approval_pending" };

describe("useVaultActionApprovals", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
  });

  it("ignores an older approval load after a newer load completes", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce([pendingApproval]);
    const { result } = renderApprovals();

    let olderLoad;
    await act(async () => {
      olderLoad = result.current.load();
      await result.current.load();
    });
    await act(async () => older.resolve([]));
    await olderLoad;

    expect(result.current.approvals.data).toEqual([pendingApproval]);
    expect(result.current.dialog.approval).toEqual(pendingApproval);
  });

  it("does not restore dialog state when a decision completes after dismissal", async () => {
    const decision = deferred();
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValue([]);
    apiPost.mockReturnValue(decision.promise);
    const { result, refreshConsoleSessions } = renderApprovals();
    await act(async () => result.current.load());

    let run;
    act(() => {
      run = result.current.run();
    });
    act(() => result.current.close());
    await act(async () => decision.resolve({ status: "completed" }));
    await run;

    expect(result.current.dialog).toEqual({ approval: null, note: "", state: "idle", error: null });
    expect(refreshConsoleSessions).not.toHaveBeenCalled();
  });

  it("does not start follow-up work when a decision completes after unmount", async () => {
    const decision = deferred();
    apiGet.mockResolvedValueOnce([pendingApproval]);
    apiPost.mockReturnValue(decision.promise);
    const { result, refreshConsoleSessions, unmount } = renderApprovals();
    await act(async () => result.current.load());

    let run;
    act(() => {
      run = result.current.run();
    });
    unmount();
    await act(async () => decision.resolve({ status: "completed" }));
    await run;

    expect(apiGet).toHaveBeenCalledOnce();
    expect(refreshConsoleSessions).not.toHaveBeenCalled();
  });

  it("turns an approval context failure into an acknowledgement-only stale state", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValue([]);
    apiPost.mockRejectedValue(new Error("Approval context changed. Review a fresh request."));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.run());

    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "stale" });
  });

  it("submits the local note and refreshes after declining", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValue([]);
    apiPost.mockResolvedValue({ status: "declined" });
    const { result } = renderApprovals();
    await act(async () => result.current.load());
    act(() => result.current.setNote("Not this time"));

    await act(async () => result.current.decline());

    expect(apiPost).toHaveBeenCalledWith("/api/vault-action-approvals/42/decline", { user_note: "Not this time" });
    expect(result.current.dialog.approval).toBeNull();
  });
});
