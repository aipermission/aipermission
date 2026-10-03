import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { APIError } from "../../lib/errors";
import { useConnectorApprovalDialog } from "../../components/console/use-connector-approval-dialog";
import type { ConnectorApproval } from "../../lib/gateway-contracts/security-contracts.ts";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

function deferred<Result>() {
  let resolve: (_value: Result) => void = () => {
    throw new Error("Deferred request is not initialized");
  };
  let reject!: (_error: Error) => void;
  const promise = new Promise<Result>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function approval(id: number, targetRef = "ssh:1:1"): ConnectorApproval {
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

type Props = Parameters<typeof useConnectorApprovalDialog>[0];
function renderDialog(options: Partial<Props> = {}) {
  const props: Props = {
    approvals: options.approvals || [],
    selectedTargetRef: options.selectedTargetRef || "ssh:1:1",
    runApproval: options.runApproval || vi.fn(),
    declineApproval: options.declineApproval || vi.fn(),
  };
  return { props, ...renderHook((next) => useConnectorApprovalDialog(next), { initialProps: props }) };
}

describe("useConnectorApprovalDialog", () => {
  beforeEach(() => vi.mocked(apiGet).mockReset());

  it("auto-opens the selected target approval and loads its exact context", async () => {
    vi.mocked(apiGet).mockResolvedValue({ ...approval(7), input: { command: "uptime" } });
    const { result } = renderDialog({ approvals: [approval(7), approval(8, "ssh:2:2")] });

    await act(async () => {});

    expect(apiGet).toHaveBeenCalledWith("/api/connector-action-approvals/7", expect.objectContaining({ signal: expect.any(AbortSignal) }));
    expect(result.current.activeApproval?.input).toEqual({ command: "uptime" });
    expect(result.current.selectedPendingApprovals).toHaveLength(1);
  });

  it("ignores detail completion after the dialog closes", async () => {
    const detail = deferred<unknown>();
    vi.mocked(apiGet).mockReturnValue(detail.promise);
    const { result } = renderDialog({ approvals: [approval(7)] });
    expect(result.current.activeApproval).toMatchObject({ id: 7 });
    expect(result.current.action.state).toBe("loading");
    act(() => result.current.close());
    await act(async () => detail.resolve({ ...approval(7), input: { command: "late" } }));

    expect(result.current.activeApproval).toBeNull();
    expect(result.current.action.state).toBe("idle");
  });

  it("rejects approval details that do not match the selected target and action", async () => {
    vi.mocked(apiGet).mockResolvedValue({ ...approval(7, "ssh:2:2"), action_name: "upload" });
    const runApproval = vi.fn();
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });

    await act(async () => {});
    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.action).toMatchObject({ state: "load_error", error: expect.stringContaining("Invalid connector approval") });
    expect(runApproval).not.toHaveBeenCalled();
  });

  it("keeps the admitted decision visible through target changes until its outcome arrives", async () => {
    const mutation = deferred<ConnectorApproval>();
    const runApproval = vi.fn(() => mutation.promise);
    vi.mocked(apiGet).mockResolvedValue(approval(7));
    const { result, rerender, props } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    act(() => void result.current.approve());
    rerender({ ...props, selectedTargetRef: "ssh:2:2", approvals: [] });
    expect(result.current.action.state).toBe("running");
    expect(result.current.activeApproval?.id).toBe(7);
    await act(async () => mutation.resolve({ ...approval(7), status: "completed" }));

    expect(result.current.activeApproval).toBeNull();
    expect(result.current.action.state).toBe("idle");
    expect(runApproval).toHaveBeenCalledWith(expect.objectContaining({ id: 7, approval_context_hash: "context-7" }), "");
  });

  it("keeps an in-flight decline visible when polling removes the approval", async () => {
    const mutation = deferred<ConnectorApproval>();
    const declineApproval = vi.fn(() => mutation.promise);
    vi.mocked(apiGet).mockResolvedValue(approval(7));
    const { result, rerender, props } = renderDialog({ approvals: [approval(7)], declineApproval });
    await act(async () => {});

    let declining: Promise<void> | undefined;
    act(() => {
      declining = result.current.decline();
    });
    rerender({ ...props, approvals: [] });

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.action.state).toBe("declining");

    await act(async () => mutation.resolve({ ...approval(7), status: "declined" }));
    await declining;
    expect(result.current.activeApproval).toBeNull();
    expect(result.current.action.state).toBe("idle");
  });

  it("keeps a stale approval visible with actionable state", async () => {
    const runApproval = vi.fn().mockRejectedValue(new APIError("Approval changed.", { code: "approval_context_changed" }));
    vi.mocked(apiGet).mockResolvedValue(approval(7));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval?.id).toBe(7);
    expect(result.current.action).toMatchObject({ state: "stale", error: "Approval changed." });
    expect(runApproval).toHaveBeenCalledWith(expect.objectContaining({ id: 7, approval_context_hash: "context-7" }), "");
  });

  it("reloads and disables a decline decision whose context became stale", async () => {
    const declineApproval = vi.fn().mockRejectedValue(new APIError("Approval changed.", { code: "approval_not_pending" }));
    vi.mocked(apiGet)
      .mockResolvedValueOnce(approval(7))
      .mockResolvedValueOnce({ ...approval(7), status: "stale", approval_context_hash: undefined });
    const { result } = renderDialog({ approvals: [approval(7)], declineApproval });
    await act(async () => {});

    await act(async () => result.current.decline());

    expect(apiGet).toHaveBeenCalledTimes(2);
    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "stale" });
    expect(result.current.action).toMatchObject({ state: "stale", error: expect.stringContaining("changed") });
  });

  it("treats a lost successful decline response as acknowledgement-only", async () => {
    const declineApproval = vi.fn().mockRejectedValue(new Error("Response was lost."));
    vi.mocked(apiGet)
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
    vi.mocked(apiGet).mockResolvedValueOnce(approval(7)).mockRejectedValueOnce(new Error("Refresh failed."));
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
    vi.mocked(apiGet).mockResolvedValue(approval(7));
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
    vi.mocked(apiGet).mockResolvedValue(approval(7));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "outcome_unknown", request_id: 7 });
    expect(result.current.action).toEqual({ state: "failed", error: "Inspect the request before retrying." });
  });

  it("reconciles a lost successful run response as acknowledgement-only", async () => {
    const runApproval = vi.fn().mockRejectedValue(new Error("Response was lost."));
    vi.mocked(apiGet)
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
    vi.mocked(apiGet).mockResolvedValueOnce(approval(7)).mockRejectedValueOnce(new Error("Gateway unavailable."));
    const { result } = renderDialog({ approvals: [approval(7)], runApproval });
    await act(async () => {});

    await act(async () => result.current.approve());

    expect(result.current.activeApproval).toMatchObject({ id: 7, status: "approval_pending" });
    expect(result.current.action).toMatchObject({ state: "failed", error: expect.stringContaining("do not retry") });
  });

  it.each(["approve", "decline"] as const)(
    "does not replace an owned %s decision with a newer review before its outcome arrives",
    async (decision) => {
      const mutation = deferred<ConnectorApproval>();
      vi.mocked(apiGet).mockResolvedValueOnce(approval(7)).mockResolvedValueOnce(approval(8));
      const { result, rerender, props } = renderDialog({
        approvals: [approval(7), approval(8)],
        runApproval: vi.fn(() => mutation.promise),
        declineApproval: vi.fn(() => mutation.promise),
      });
      await act(async () => {});
      let pending: Promise<void> | undefined;
      act(() => {
        pending = result.current[decision]();
      });
      await act(async () => result.current.open(approval(8)));
      act(() => result.current.setNote("For the newer decision"));
      expect(result.current.activeApproval?.id).toBe(7);
      expect(result.current.note).toBe("");
      expect(apiGet).toHaveBeenCalledOnce();
      rerender({ ...props, approvals: [approval(8)] });
      await act(async () => mutation.resolve({ ...approval(7), status: "completed" }));
      await pending;
      expect(result.current.activeApproval?.id).toBe(8);
      expect(result.current.note).toBe("");
      expect(result.current.action.state).toBe("idle");
    },
  );
});

it("shows a fetched terminal approval and ignores decisions after the dialog closes", async () => {
  vi.mocked(apiGet)
    .mockReset()
    .mockResolvedValue({ ...approval(7), status: "completed" });
  const runApproval = vi.fn();
  const declineApproval = vi.fn();
  const { result } = renderDialog({ approvals: [approval(7)], runApproval, declineApproval });
  await act(async () => {});
  expect(result.current.activeApproval).toMatchObject({ id: 7, status: "completed" });
  expect(result.current.action).toMatchObject({ state: "failed", error: expect.stringContaining("no longer pending") });
  act(() => result.current.close());
  await act(async () => {
    await result.current.approve();
    await result.current.decline();
  });
  expect(runApproval).not.toHaveBeenCalled();
  expect(declineApproval).not.toHaveBeenCalled();
});

it("does not submit decisions while exact approval detail is still loading", async () => {
  const detail = deferred<unknown>();
  vi.mocked(apiGet).mockReset().mockReturnValue(detail.promise);
  const runApproval = vi.fn();
  const declineApproval = vi.fn();
  const { result } = renderDialog({ approvals: [approval(7)], runApproval, declineApproval });
  expect(result.current.action.state).toBe("loading");
  await act(async () => {
    await result.current.approve();
    await result.current.decline();
  });
  expect(runApproval).not.toHaveBeenCalled();
  expect(declineApproval).not.toHaveBeenCalled();
  await act(async () => detail.resolve(approval(7)));
  expect(result.current.action.state).toBe("idle");
});

it.each(["completed", "running"] as const)("closes a successfully %s approval and permits the next decision", async (status) => {
  vi.mocked(apiGet).mockReset().mockResolvedValueOnce(approval(7)).mockResolvedValueOnce(approval(8));
  const runApproval = vi.fn();
  const { result, rerender, props } = renderDialog({ approvals: [approval(7)], runApproval });
  runApproval.mockImplementationOnce(async () => {
    rerender({ ...props, approvals: [] });
    return { ...approval(7), status };
  });
  await act(async () => {});
  await act(async () => result.current.approve());
  expect(result.current.activeApproval).toBeNull();
  expect(result.current.action).toEqual({ state: "idle", error: null });
  await act(async () => rerender({ ...props, approvals: [approval(8)] }));
  expect(result.current.activeApproval).toMatchObject({ id: 8, status: "approval_pending" });
  expect(apiGet).toHaveBeenLastCalledWith(
    "/api/connector-action-approvals/8",
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});

it("clears an idle reviewed decision when polling removes its pending approval", async () => {
  vi.mocked(apiGet).mockReset().mockResolvedValue(approval(7));
  const { result, rerender, props } = renderDialog({ approvals: [approval(7)] });
  await act(async () => {});
  expect(result.current.activeApproval?.id).toBe(7);
  act(() => result.current.setNote("Reviewed draft"));
  rerender({ ...props, approvals: [] });
  expect(result.current.activeApproval).toBeNull();
  expect(result.current.note).toBe("");
  expect(result.current.action).toEqual({ state: "idle", error: null });
});

it.each(["approve", "decline"] as const)("keeps a failed %s reconciliation visible when close is attempted", async (decision) => {
  const reconciliation = deferred<unknown>();
  vi.mocked(apiGet).mockReset().mockResolvedValueOnce(approval(7)).mockReturnValueOnce(reconciliation.promise);
  const runApproval = vi.fn().mockRejectedValue(new Error("Decision response lost"));
  const declineApproval = vi.fn().mockRejectedValue(new Error("Decision response lost"));
  const { result } = renderDialog({ approvals: [approval(7)], runApproval, declineApproval });
  await act(async () => {});
  let pending!: Promise<void>;
  await act(async () => {
    pending = result.current[decision]();
  });
  expect(apiGet).toHaveBeenCalledTimes(2);
  act(() => result.current.close());
  expect(result.current.activeApproval?.id).toBe(7);
  await act(async () => {
    reconciliation.reject(new Error("Retired detail failed"));
    await pending;
  });
  expect(result.current.activeApproval?.id).toBe(7);
  expect(result.current.action.error).toContain("Decision response lost");
  act(() => result.current.close());
  expect(result.current.activeApproval).toBeNull();
  expect(result.current.action).toEqual({ state: "idle", error: null });
});

it.each(["approve", "decline"] as const)("serializes same-tick %s and its competing decision", async (decision) => {
  const mutation = deferred<ConnectorApproval>();
  vi.mocked(apiGet).mockReset().mockResolvedValue(approval(7));
  const runApproval = vi.fn(() => mutation.promise);
  const declineApproval = vi.fn(() => mutation.promise);
  const { result, rerender, props } = renderDialog({ approvals: [approval(7)], runApproval, declineApproval });
  await act(async () => {});
  let pending!: Promise<void>;
  act(() => {
    const reviewed = result.current;
    pending = reviewed[decision]();
    void reviewed.approve();
    void reviewed.decline();
    reviewed.close();
  });
  expect(runApproval.mock.calls.length + declineApproval.mock.calls.length).toBe(1);
  rerender({ ...props, approvals: [] });
  await act(async () => {
    mutation.resolve({ ...approval(7), status: "completed" });
    await pending;
  });
  expect(result.current.activeApproval).toBeNull();
});

it.each(["approve", "decline"] as const)("retires %s completion and rejection after unmount", async (decision) => {
  for (const outcome of ["resolve", "reject"] as const) {
    const mutation = deferred<ConnectorApproval>();
    vi.mocked(apiGet).mockReset().mockResolvedValue(approval(7));
    const { result, unmount } = renderDialog({
      approvals: [approval(7)],
      runApproval: vi.fn(() => mutation.promise),
      declineApproval: vi.fn(() => mutation.promise),
    });
    await act(async () => {});
    let pending!: Promise<void>;
    act(() => {
      pending = result.current[decision]();
    });
    unmount();
    await act(async () => {
      if (outcome === "resolve") mutation.resolve({ ...approval(7), status: "completed" });
      else mutation.reject(new Error("Retired decision"));
      await pending;
    });
    expect(apiGet).toHaveBeenCalledOnce();
  }
});

it.each(["approve", "decline"] as const)("retires %s reconciliation after unmount", async (decision) => {
  const detail = deferred<unknown>();
  vi.mocked(apiGet).mockReset().mockResolvedValueOnce(approval(7)).mockReturnValueOnce(detail.promise);
  const { result, unmount } = renderDialog({
    approvals: [approval(7)],
    runApproval: vi.fn().mockRejectedValue(new Error("Response lost")),
    declineApproval: vi.fn().mockRejectedValue(new Error("Response lost")),
  });
  await act(async () => {});
  let pending!: Promise<void>;
  await act(async () => {
    pending = result.current[decision]();
  });
  expect(apiGet).toHaveBeenCalledTimes(2);
  unmount();
  await act(async () => {
    detail.resolve(approval(7));
    await pending;
  });
  expect(result.current.action.state).toBe(decision === "approve" ? "running" : "declining");
});
