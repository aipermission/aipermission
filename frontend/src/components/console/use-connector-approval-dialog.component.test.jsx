import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
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
  return { id, target_ref: targetRef, status: "approval_pending", preview: {}, input: {} };
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
  });

  it("keeps a stale approval visible with actionable state", async () => {
    const runApproval = vi.fn().mockRejectedValue(new Error("Approval context is stale; create a fresh request."));
    apiGet.mockResolvedValue(approval(7));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval.id).toBe(7);
    expect(result.current.action).toMatchObject({ state: "stale", error: expect.stringContaining("stale") });
  });
});
