import { useCallback, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { failedResource, pollReadOptions } from "../../lib/async-resource";
import { useRequestGuard } from "../../lib/request-guard";
import { vaultApproval, vaultApprovals } from "../../lib/gateway-contracts/security-contracts";
import { reconcileVaultApprovalDialog } from "../../lib/vault-approval-poll";

const initialApprovals = { state: "loading", data: [], error: null };
const initialDialog = { approval: null, note: "", state: "idle", error: null };

function isStaleApprovalError(error) {
  return ["approval_context_changed", "approval_not_pending"].includes(error?.code);
}

export function useVaultActionApprovals({ pollIsCurrent, refreshConsoleSessions }) {
  const [approvals, setApprovals] = useState(initialApprovals);
  const [dialog, setDialog] = useState(initialDialog);
  const seenPendingRef = useRef(new Set());
  const requests = useRequestGuard("vault-action-approvals");

  const load = useCallback(
    async (generation) => {
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
    if (!approval || approvals.state !== "ready") return;
    const request = requests.begin("decision");
    setDialog((current) => ({ ...current, state: "running", error: null }));
    try {
      vaultApproval(
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
      setDialog(initialDialog);
      await Promise.all([load(), refreshConsoleSessions()]);
    } catch (error) {
      if (!request.isCurrent()) return;
      const exact = await readExactVaultApproval(approval, request.signal);
      if (!request.isCurrent()) return;
      await load();
      if (!request.isCurrent()) return;
      if (exact?.status === "completed") await refreshConsoleSessions();
      if (!request.isCurrent()) return;
      setDialog((current) => ({
        ...current,
        approval: exact || current.approval || approval,
        state: exact && exact.status !== "approval_pending" ? "stale" : isStaleApprovalError(error) ? "stale" : "failed",
        error:
          exact && exact.status !== "approval_pending"
            ? `${error.message} This Vault approval is ${exact.status}; the run response may have been lost.`
            : error.message,
      }));
    } finally {
      request.complete();
    }
  }, [approvals.state, dialog.approval, dialog.note, load, refreshConsoleSessions, requests]);

  const decline = useCallback(async () => {
    const approval = dialog.approval;
    if (!approval || approvals.state !== "ready") return;
    const request = requests.begin("decision");
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
      setDialog(initialDialog);
      await load();
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
          ? `${error.message} This Vault approval is no longer pending; the decline may already have been recorded.`
          : error.message,
      }));
    } finally {
      request.complete();
    }
  }, [approvals.state, dialog.approval, dialog.note, load, requests]);

  const close = useCallback(() => {
    requests.invalidate("decision");
    setDialog(initialDialog);
  }, [requests]);

  const openPending = useCallback(() => {
    if (approvals.state !== "ready" || dialog.approval) return;
    const approval = approvals.data.find((item) => item.status === "approval_pending");
    if (!approval) return;
    seenPendingRef.current.add(approval.id);
    setDialog({ ...initialDialog, approval });
  }, [approvals, dialog.approval]);

  const setNote = useCallback((note) => setDialog((current) => ({ ...current, note })), []);

  return { approvals, close, decline, dialog, load, openPending, run, setNote };
}

async function readExactVaultApproval(approval, signal) {
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
