import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { APIError } from "../../lib/errors";
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
  approval_context: {
    schema: "vault-action-v3",
    action_name: "generate_item",
    token_id: 7,
    project_id: 3,
    workspace_id: "fixture-workspace",
    runtime_instance_id: "fixture-runtime",
    capability_name: "vault_item_generate",
    execution_rule: "prompt",
    input_hash: "input-hash",
    project_scope_hash: "scope-hash",
  },
  approval_context_hash: "context-hash",
  idempotency_key: "fixture-key",
  created_at: "2026-09-16T00:00:00Z",
  expires_at: "2026-09-16T00:15:00Z",
  updated_at: "2026-09-16T00:00:00Z",
};

function decisionApproval(status, overrides = {}) {
  return { ...pendingApproval, status, approval_context_hash: "", ...overrides };
}

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

  it("ignores an older approval load failure after a newer load completes", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce([pendingApproval]);
    const { result } = renderApprovals();

    let olderLoad;
    await act(async () => {
      olderLoad = result.current.load();
      await result.current.load();
    });
    await act(async () => older.reject(new Error("late failure")));
    await olderLoad;

    expect(result.current.approvals).toEqual({ state: "ready", data: [pendingApproval], error: null });
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

    await act(async () => decision.resolve(decisionApproval("completed")));
    await run;
    expect(result.current.dialog).toEqual({ approval: null, note: "", state: "idle", error: null });
  });

  it("reopens a dismissed pending approval for an explicit decision", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce([]);
    apiPost.mockResolvedValue(decisionApproval("completed"));
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
    await act(async () => decision.resolve(decisionApproval("completed")));
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
    await act(async () => decision.resolve(decisionApproval("completed")));
    await run;

    expect(apiGet).toHaveBeenCalledOnce();
    expect(refreshConsoleSessions).not.toHaveBeenCalled();
  });

  it("turns an approval context failure into an acknowledgement-only stale state", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValue([]);
    apiPost.mockRejectedValue(new APIError("Approval changed.", { code: "approval_context_changed" }));
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
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce(decisionApproval("stale")).mockResolvedValueOnce([]);
    apiPost.mockRejectedValue(new APIError("Approval changed.", { code: "approval_not_pending" }));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.decline());

    expect(apiGet).toHaveBeenCalledTimes(3);
    expect(result.current.dialog).toMatchObject({ approval: { id: 42, status: "stale" }, state: "stale" });
  });

  it("allows a fresh decline decision when reconciliation still shows the request pending", async () => {
    const refreshed = { ...pendingApproval, approval_context_hash: "new-context-hash" };
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce(refreshed).mockResolvedValueOnce([refreshed]);
    apiPost.mockRejectedValue(new APIError("Approval changed.", { code: "approval_context_changed" }));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.decline());

    expect(result.current.dialog).toMatchObject({ approval: refreshed, state: "error", error: "Approval changed." });
  });

  it("makes a lost successful decline response acknowledgement-only when the request disappeared", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce(decisionApproval("declined")).mockResolvedValueOnce([]);
    apiPost.mockRejectedValue(new Error("Response was lost."));
    const { result } = renderApprovals();
    await act(async () => result.current.load());
    act(() => result.current.setNote("Reviewed locally"));

    await act(async () => result.current.decline());

    expect(result.current.dialog).toMatchObject({
      approval: { id: 42, status: "declined" },
      note: "Reviewed locally",
      state: "stale",
      error: expect.stringContaining("already"),
    });
  });

  it("reconciles a lost successful run response through the exact request", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce(decisionApproval("completed")).mockResolvedValueOnce([]);
    apiPost.mockRejectedValue(new Error("Response was lost."));
    const { result, refreshConsoleSessions } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.run());

    expect(apiGet).toHaveBeenNthCalledWith(2, "/api/vault-action-approvals/42", expect.any(Object));
    expect(refreshConsoleSessions).toHaveBeenCalledTimes(1);
    expect(result.current.dialog).toMatchObject({ approval: { id: 42, status: "completed" }, state: "stale" });
  });

  it("does not infer a terminal decline from an absent capped-list entry", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValueOnce(pendingApproval).mockResolvedValueOnce([]);
    apiPost.mockRejectedValue(new Error("Decline response was lost."));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.decline());

    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "error", error: "Decline response was lost." });
  });

  it("does not open a review when no pending approval exists", async () => {
    apiGet.mockResolvedValue([]);
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    act(() => result.current.openPending());

    expect(result.current.dialog.approval).toBeNull();
  });

  it("does not decline before a pending approval is loaded", async () => {
    const { result } = renderApprovals();

    await act(async () => result.current.decline());

    expect(apiPost).not.toHaveBeenCalled();
    expect(result.current.dialog).toEqual({ approval: null, note: "", state: "idle", error: null });
  });

  it("submits the local note and refreshes after declining", async () => {
    apiGet.mockResolvedValueOnce([pendingApproval]).mockResolvedValue([]);
    apiPost.mockResolvedValue(decisionApproval("declined"));
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

  it("keeps the approval visible when a run returns a malformed success envelope", async () => {
    apiGet.mockResolvedValue([pendingApproval]);
    apiPost.mockResolvedValue({ id: pendingApproval.id, status: "completed" });
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.run());

    expect(result.current.dialog).toMatchObject({
      approval: pendingApproval,
      state: "failed",
      error: expect.stringContaining("Invalid Vault approval decision"),
    });
  });

  it("keeps the approval visible when a decision response belongs to another request", async () => {
    apiGet.mockResolvedValue([pendingApproval]);
    apiPost.mockResolvedValue(decisionApproval("completed", { id: pendingApproval.id + 1 }));
    const { result } = renderApprovals();
    await act(async () => result.current.load());

    await act(async () => result.current.run());

    expect(result.current.dialog).toMatchObject({ approval: pendingApproval, state: "failed" });
  });
});
