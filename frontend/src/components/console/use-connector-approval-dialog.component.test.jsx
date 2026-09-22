import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { APIError } from "../../lib/errors";
import { useConnectorApprovalDialog } from "./use-connector-approval-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function approval(id, targetRef = "ssh:1:1") {
  return {
    id,
    target_id: 1,
    target_name: "fixture",
    target_ref: targetRef,
    profile_id: 1,
    profile_label: "default",
    connector_kind: "ssh",
    action_name: "exec",
    status: "approval_pending",
    approval_context_hash: `context-${id}`,
    retry_policy: { class: "non_idempotent", guidance: "Inspect before retrying." },
    created_at: "2026-09-16T00:00:00Z",
    preview: {},
    input: {},
  };
}

function renderDialog(options = {}) {
  const props = {
    approvals: options.approvals || [],
    selectedTargetRef: options.selectedTargetRef || "ssh:1:1",
    runApproval: options.runApproval || vi.fn(),
    declineApproval: options.declineApproval || vi.fn(),
  };
  return { props, ...renderHook((next) => useConnectorApprovalDialog(next), { initialProps: props }) };
}

describe("useConnectorApprovalDialog", () => {
  beforeEach(() => apiGet.mockReset());

  it("auto-opens the selected target approval and loads its exact context", async () => {
    apiGet.mockResolvedValue({ ...approval(7), input: { command: "uptime" } });
    const { result } = renderDialog({ approvals: [approval(7), approval(8, "ssh:2:2")] });

    await act(async () => {});

    expect(apiGet).toHaveBeenCalledWith("/api/connector-action-approvals/7", expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(result.current.activeApproval.input).toEqual({ command: "uptime" });
    expect(result.current.selectedPendingApprovals).toHaveLength(1);
  });

  it("ignores detail completion after the dialog closes", async () => {
    const detail = deferred();
    apiGet.mockReturnValue(detail.promise);
    const { result } = renderDialog();

    act(() => void result.current.open(approval(7)));
    act(() => result.current.close());
    await act(async () => detail.resolve({ ...approval(7), input: { command: "late" } }));

    expect(result.current.activeApproval).toBeNull();
    expect(result.current.action.state).toBe("idle");
  });

  it("rejects approval details that do not match the selected target and action", async () => {
    apiGet.mockResolvedValue({ ...approval(7, "ssh:2:2"), action_name: "upload" });
    const runApproval = vi.fn();
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });

    await act(async () => {});
    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.action).toMatchObject({ state: "load_error", error: expect.stringContaining("Invalid connector approval") });
    expect(runApproval).not.toHaveBeenCalled();
  });

  it("does not apply a mutation completion after the selected target changes", async () => {
    const mutation = deferred();
    const runApproval = vi.fn(() => mutation.promise);
    apiGet.mockResolvedValue(approval(7));
    const { result, rerender, props } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    act(() => void result.current.approve());
    rerender({ ...props, selectedTargetRef: "ssh:2:2", approvals: [] });
    await act(async () => mutation.resolve({ status: "completed" }));

    expect(result.current.activeApproval).toBeNull();
    expect(result.current.action.state).toBe("idle");
    expect(runApproval).toHaveBeenCalledWith(expect.objectContaining({ id: 7, approval_context_hash: "context-7" }), "");
  });

  it("keeps an in-flight decline visible when polling removes the approval", async () => {
    const mutation = deferred();
    const declineApproval = vi.fn(() => mutation.promise);
    apiGet.mockResolvedValue(approval(7));
    const { result, rerender, props } = renderDialog({ approvals: [approval(7)], declineApproval });
    await act(async () => {});

    let declining;
    act(() => {
      declining = result.current.decline();
    });
    rerender({ ...props, approvals: [] });

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.action.state).toBe("declining");

    await act(async () => mutation.resolve({ status: "declined" }));
    await declining;
    expect(result.current.activeApproval).toBeNull();
    expect(result.current.action.state).toBe("idle");
  });

  it("keeps a stale approval visible with actionable state", async () => {
    const runApproval = vi.fn().mockRejectedValue(new APIError("Approval changed.", { code: "approval_context_changed" }));
    apiGet.mockResolvedValue(approval(7));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval.id).toBe(7);
    expect(result.current.action).toMatchObject({ state: "stale", error: "Approval changed." });
    expect(runApproval).toHaveBeenCalledWith(expect.objectContaining({ id: 7, approval_context_hash: "context-7" }), "");
  });

  it("reloads and disables a decline decision whose context became stale", async () => {
    const declineApproval = vi.fn().mockRejectedValue(new APIError("Approval changed.", { code: "approval_not_pending" }));
    apiGet.mockResolvedValueOnce(approval(7)).mockResolvedValueOnce({ ...approval(7), status: "stale", approval_context_hash: undefined });
    const { result } = renderDialog({ approvals: [approval(7)], declineApproval });
    await act(async () => {});

    await act(async () => result.current.decline());

    expect(apiGet).toHaveBeenCalledTimes(2);
    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "stale" });
    expect(result.current.action).toMatchObject({ state: "stale", error: expect.stringContaining("changed") });
  });

  it("treats a lost successful decline response as acknowledgement-only", async () => {
    const declineApproval = vi.fn().mockRejectedValue(new Error("Response was lost."));
    apiGet
      .mockResolvedValueOnce(approval(7))
      .mockResolvedValueOnce({ ...approval(7), status: "declined", approval_context_hash: undefined });
    const { result } = renderDialog({ approvals: [approval(7)], declineApproval });
    await act(async () => {});
    act(() => result.current.setNote("Reviewed locally"));

    await act(async () => result.current.decline());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "declined" });
    expect(result.current.note).toBe("Reviewed locally");
    expect(result.current.action).toMatchObject({ state: "stale", error: expect.stringContaining("already") });
  });

  it("preserves the reviewed decline context when reconciliation also fails", async () => {
    const declineApproval = vi.fn().mockRejectedValue(new Error("Decline failed."));
    apiGet.mockResolvedValueOnce(approval(7)).mockRejectedValueOnce(new Error("Refresh failed."));
    const { result } = renderDialog({ approvals: [approval(7)], declineApproval });
    await act(async () => {});
    act(() => result.current.setNote("Keep this note"));

    await act(async () => result.current.decline());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.note).toBe("Keep this note");
    expect(result.current.action).toEqual({ state: "error", error: "Decline failed." });
  });

  it("keeps non-success run outcomes visible instead of closing the decision", async () => {
    const runApproval = vi.fn().mockResolvedValue({ ...approval(7), status: "outcome_unknown", error: "Inspect target state." });
    apiGet.mockResolvedValue(approval(7));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "outcome_unknown" });
    expect(result.current.action).toEqual({ state: "failed", error: "Inspect target state." });
  });

  it("makes an HTTP outcome-unknown run acknowledgement-only", async () => {
    const runApproval = vi.fn().mockRejectedValue(
      new APIError("Persistence outcome is unknown.", {
        status: 503,
        data: {
          status: "outcome_unknown",
          request_id: 7,
          error: "Persistence outcome is unknown.",
          assistant_hint: "Inspect the request before retrying.",
        },
      }),
    );
    apiGet.mockResolvedValue(approval(7));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "outcome_unknown", request_id: 7 });
    expect(result.current.action).toEqual({ state: "failed", error: "Inspect the request before retrying." });
  });

  it("reconciles a lost successful run response as acknowledgement-only", async () => {
    const runApproval = vi.fn().mockRejectedValue(new Error("Response was lost."));
    apiGet
      .mockResolvedValueOnce(approval(7))
      .mockResolvedValueOnce({ ...approval(7), status: "completed", approval_context_hash: undefined });
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(apiGet).toHaveBeenCalledTimes(2);
    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "completed" });
    expect(result.current.action).toMatchObject({ state: "stale", error: expect.stringContaining("may have been lost") });
  });

  it("blocks retry when a lost run response cannot be reconciled", async () => {
    const runApproval = vi.fn().mockRejectedValue(new Error("Response was lost."));
    apiGet.mockResolvedValueOnce(approval(7)).mockRejectedValueOnce(new Error("Gateway unavailable."));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.action).toMatchObject({ state: "failed", error: expect.stringContaining("do not retry") });
  });
});
