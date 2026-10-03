import { useCallback, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { failedResource, pollReadOptions } from "../../lib/async-resource";
import { useRequestGuard } from "../../lib/request-guard";
import { useApprovalDecisionOwner } from "../../lib/use-approval-decision-owner";
import { vaultApproval, vaultApprovals } from "../../lib/gateway-contracts/security-contracts";
import { reconcileVaultApprovalDialog } from "../../lib/vault-approval-poll";
import type { ApprovalDialog } from "../../lib/vault-approval-poll.ts";
import type { VaultApproval } from "../../lib/gateway-contracts/security-contracts.ts";
import { errorMessage } from "../../lib/errors.ts";

type ApprovalsResource = { state: "loading" | "ready" | "error"; data: VaultApproval[]; error: string | null };
export type VaultApprovalDialogState = ApprovalDialog<VaultApproval>;
type Options = { pollIsCurrent: (_generation?: number) => boolean; refreshConsoleSessions: () => unknown | Promise<unknown> };
const initialApprovals: ApprovalsResource = { state: "loading", data: [], error: null };
const initialDialog: VaultApprovalDialogState = { approval: null, note: "", state: "idle", error: null };

function isStaleApprovalError(error: unknown) {
  return (
    !!error &&
    typeof error === "object" &&
    "code" in error &&
    typeof error.code === "string" &&
    ["approval_context_changed", "approval_not_pending"].includes(error.code)
  );
}

export function useVaultActionApprovals({ pollIsCurrent, refreshConsoleSessions }: Options) {
  const [approvals, setApprovals] = useState(initialApprovals);
  const [dialog, setDialog] = useState(initialDialog);
  const seenPendingRef = useRef(new Set<number>());
  const requests = useRequestGuard("vault-action-approvals");
  const decision = useApprovalDecisionOwner(requests, "decision");

  const load = useCallback(
    async (generation?: number) => {
      const request = requests.begin("load");
      try {
        const data = await apiGet("/api/vault-action-approvals?status=approval_pending", pollReadOptions(request.signal, generation));
        if (!request.isCurrent() || !pollIsCurrent(generation)) return null;
        const verified = vaultApprovals(data);
        setApprovals({ state: "ready", data: verified, error: null });
        const pending = verified.filter((item) => item.status === "approval_pending");
        setDialog((current) =>
          reconcileVaultApprovalDialog(
            current.state === "load_error" ? { ...current, state: "idle", error: null } : current,
            pending,
            seenPendingRef.current,
          ),
        );
        return verified;
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return null;
        setApprovals((current) => failedResource(current, error));
        setDialog((current) =>
          current.approval && ["idle", "error"].includes(current.state)
            ? { ...current, state: "load_error", error: "Approval refresh failed. Refresh before making a decision." }
            : current,
        );
        return null;
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const run = useCallback(async () => {
    const approval = dialog.approval;
    if (!approval || approvals.state !== "ready" || !["idle", "error"].includes(dialog.state)) return;
    const request = decision.begin();
    if (!request) return;
    setDialog((current) => ({ ...current, state: "running", error: null }));
    try {
      const completed = vaultApproval(
        await apiPost(`/api/vault-action-approvals/${approval.id}/run`, {
          user_note: dialog.note,
          approval_context_hash: approval.approval_context_hash,
        }),
        {
          id: approval.id,
          tokenID: approval.token_id,
          projectID: approval.project_id,
          actionName: approval.action_name,
          statuses: ["completed"],
        },
        "Vault approval decision",
      );
      if (!request.isCurrent()) return;
      const refreshError = await refreshVaultDecision(load, refreshConsoleSessions);
      if (!request.isCurrent()) return;
      setDialog(refreshError ? { approval: completed, note: dialog.note, state: "stale", error: refreshError } : initialDialog);
    } catch (error) {
      if (!request.isCurrent()) return;
      const exact = await readExactVaultApproval(approval, request.signal);
      if (!request.isCurrent()) return;
      const refreshError = await refreshVaultDecision(load, exact?.status === "completed" ? refreshConsoleSessions : undefined);
      if (!request.isCurrent()) return;
      setDialog((current) => ({
        ...current,
        approval: exact || current.approval || approval,
        state: exact && exact.status !== "approval_pending" ? "stale" : isStaleApprovalError(error) ? "stale" : "failed",
        error:
          exact && exact.status !== "approval_pending"
            ? `${errorMessage(error)} This Vault approval is ${exact.status}; the run response may have been lost.${refreshError ? ` ${refreshError}` : ""}`
            : errorMessage(error),
      }));
    } finally {
      decision.complete(request);
    }
  }, [approvals.state, decision, dialog.approval, dialog.note, dialog.state, load, refreshConsoleSessions]);

  const decline = useCallback(async () => {
    const approval = dialog.approval;
    if (!approval || approvals.state !== "ready" || !["idle", "error"].includes(dialog.state)) return;
    const request = decision.begin();
    if (!request) return;
    setDialog((current) => ({ ...current, state: "declining", error: null }));
    try {
      vaultApproval(
        await apiPost(`/api/vault-action-approvals/${approval.id}/decline`, {
          user_note: dialog.note,
          approval_context_hash: approval.approval_context_hash,
        }),
        {
          id: approval.id,
          tokenID: approval.token_id,
          projectID: approval.project_id,
          actionName: approval.action_name,
          statuses: ["declined"],
        },
        "Vault approval decision",
      );
      if (!request.isCurrent()) return;
      await load();
      if (request.isCurrent()) setDialog(initialDialog);
    } catch (error) {
      if (!request.isCurrent()) return;
      const exact = await readExactVaultApproval(approval, request.signal);
      if (!request.isCurrent()) return;
      await load();
      if (!request.isCurrent()) return;
      const terminalAfterRefresh = exact && exact.status !== "approval_pending";
      setDialog((current) => ({
        ...current,
        approval: exact || current.approval || approval,
        state: terminalAfterRefresh || (!exact && isStaleApprovalError(error)) ? "stale" : "error",
        error: terminalAfterRefresh
          ? `${errorMessage(error)} This Vault approval is no longer pending; the decline may already have been recorded.`
          : errorMessage(error),
      }));
    } finally {
      decision.complete(request);
    }
  }, [approvals.state, decision, dialog.approval, dialog.note, dialog.state, load]);

  const close = useCallback(() => {
    if (decision.isPending()) return;
    requests.invalidate("decision");
    setDialog(initialDialog);
  }, [decision, requests]);

  const openPending = useCallback(() => {
    if (approvals.state !== "ready" || dialog.approval) return;
    const approval = approvals.data.find((item) => item.status === "approval_pending");
    if (!approval) return;
    seenPendingRef.current.add(approval.id);
    setDialog({ ...initialDialog, approval });
  }, [approvals, dialog.approval]);

  const setNote = useCallback(
    (note: string) => {
      if (!decision.isPending()) setDialog((current) => ({ ...current, note }));
    },
    [decision],
  );

  return { approvals, close, decline, dialog, load, openPending, run, setNote };
}

async function refreshVaultDecision(load: () => Promise<VaultApproval[] | null>, refreshSessions?: Options["refreshConsoleSessions"]) {
  const [approvals, sessions] = await Promise.allSettled([load(), Promise.resolve().then(() => refreshSessions?.())]);
  if (sessions.status === "rejected") {
    return `Vault approval completed, but refreshing sessions failed: ${errorMessage(sessions.reason)} Refresh the Console before continuing.`;
  }
  return approvals.status === "rejected" || approvals.value === null
    ? "Vault approval completed, but refreshing pending approvals failed. Refresh activity before continuing."
    : null;
}

async function readExactVaultApproval(approval: VaultApproval, signal: AbortSignal) {
  try {
    return vaultApproval(
      await apiGet(`/api/vault-action-approvals/${approval.id}`, { signal }),
      {
        id: approval.id,
        tokenID: approval.token_id,
        projectID: approval.project_id,
        actionName: approval.action_name,
      },
      "Vault approval reconciliation",
    );
  } catch {
    return null;
  }
}
