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

const pendingApproval = {
  id: 42,
  status: "approval_pending",
  token_id: 7,
  token_name: "codex",
  project_id: 3,
  project_name: "My Project",
  project_slug: "my-project",
  action_name: "generate_item",
  source: "mcp",
  input: {},
  reason: "coverage",
  approval_context_hash: "context-hash",
  idempotency_key: "fixture-key",
  created_at: "2026-09-16T00:00:00Z",
  expires_at: "2026-09-16T00:15:00Z",
  updated_at: "2026-09-16T00:00:00Z",
};

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

  it("rejects malformed approval data without opening an approval dialog", async () => {
    apiGet.mockResolvedValueOnce([{ id: "not-an-id", status: "approval_pending" }]);
    const { result } = renderApprovals();

    await act(async () => result.current.load());

    expect(result.current.approvals).toMatchObject({ state: "error", data: [] });
    expect(result.current.approvals.error).toContain("Invalid Vault approvals response from gateway.");
    expect(result.current.dialog.approval).toBeNull();
  });

  it("keeps approval context read-only through a transient failure and recovers", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce([pendingApproval]);
    const { result } = renderApprovals();

    await act(async () => result.current.load());
    await act(async () => result.current.load());
    expect(result.current.approvals).toEqual({ state: "error", data: [pendingApproval], error: "offline" });
    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "load_error" });

    await act(async () => result.current.run());
    expect(apiPost).not.toHaveBeenCalled();

    await act(async () => result.current.load());
    expect(result.current.approvals).toEqual({ state: "ready", data: [pendingApproval], error: null });
    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "idle", error: null });
  });

  it("does not replace an in-flight decision with a poll error", async () => {
    const decision = deferred();
    apiGet.mockResolvedValueOnce([pendingApproval]).mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce([]);
    apiPost.mockReturnValue(decision.promise);
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    let run;
    act(() => {
      run = result.current.run();
    });
    await act(async () => result.current.load());
    expect(result.current.dialog.state).toBe("running");

    await act(async () => decision.resolve({ status: "completed" }));
    await run;
    expect(result.current.dialog).toEqual({ approval: null, note: "", state: "idle", error: null });
  });

  it("reopens a dismissed pending approval for an explicit decision", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce([]);
    apiPost.mockResolvedValue({ status: "completed" });
    const { result } = renderApprovals();
    await act(async () => result.current.load());
    act(() => result.current.close());
    await act(async () => result.current.load());
    expect(result.current.dialog.approval).toBeNull();

    act(() => result.current.openPending());
    expect(result.current.dialog.approval).toEqual(pendingApproval);
    await act(async () => result.current.run());
    expect(apiPost).toHaveBeenCalledWith("/api/vault-action-approvals/42/run", {
      user_note: "",
      approval_context_hash: "context-hash",
    });
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

  it("keeps a non-stale failed decision visible for review", async () => {
    apiGet.mockResolvedValue([pendingApproval]);
    apiPost.mockRejectedValue(new Error("Delivery failed"));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.run());

    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "failed", error: "Delivery failed" });
  });

  it("preserves an approval after a declined decision fails", async () => {
    apiGet.mockResolvedValue([pendingApproval]);
    apiPost.mockRejectedValue(new Error("Decline failed"));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.decline());

    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "error", error: "Decline failed" });
  });

  it("reloads and makes a stale decline conflict acknowledgement-only", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce([]);
    apiPost.mockRejectedValue(new Error("Approval context changed. Review a fresh request."));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.decline());

    expect(apiGet).toHaveBeenCalledTimes(2);
    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "stale" });
  });

  it("does not open a review when no pending approval exists", async () => {
    apiGet.mockResolvedValue([]);
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    act(() => result.current.openPending());

    expect(result.current.dialog.approval).toBeNull();
  });

  it("submits the local note and refreshes after declining", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValue([]);
    apiPost.mockResolvedValue({ status: "declined" });
    const { result } = renderApprovals();
    await act(async () => result.current.load());
    act(() => result.current.setNote("Not this time"));

    await act(async () => result.current.decline());

    expect(apiPost).toHaveBeenCalledWith("/api/vault-action-approvals/42/decline", {
      user_note: "Not this time",
      approval_context_hash: "context-hash",
    });
    expect(result.current.dialog.approval).toBeNull();
  });
});
