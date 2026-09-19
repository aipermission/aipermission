import { useCallback, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { failedResource, pollReadOptions } from "../../lib/async-resource";
import { useRequestGuard } from "../../lib/request-guard";
import { vaultApprovals } from "../../lib/gateway-contracts/security-contracts";
import { reconcileVaultApprovalDialog } from "../../lib/vault-approval-poll";

const initialApprovals = { state: "loading", data: [], error: null };
const initialDialog = { approval: null, note: "", state: "idle", error: null };

function isStaleApprovalError(error) {
  const message = String(error?.message || "").toLowerCase();
  return message.includes("stale") || message.includes("changed") || message.includes("fresh request");
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
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
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
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setApprovals((current) => failedResource(current, error));
        setDialog((current) =>
          current.approval && ["idle", "error"].includes(current.state)
            ? { ...current, state: "load_error", error: "Approval refresh failed. Refresh before making a decision." }
            : current,
        );
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
      await apiPost(`/api/vault-action-approvals/${approval.id}/run`, {
        user_note: dialog.note,
        approval_context_hash: approval.approval_context_hash,
      });
      if (!request.isCurrent()) return;
      setDialog(initialDialog);
      await Promise.all([load(), refreshConsoleSessions()]);
    } catch (error) {
      if (!request.isCurrent()) return;
      await load();
      if (!request.isCurrent()) return;
      setDialog((current) => ({
        ...current,
        state: isStaleApprovalError(error) ? "stale" : "failed",
        error: error.message,
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
      await apiPost(`/api/vault-action-approvals/${approval.id}/decline`, {
        user_note: dialog.note,
        approval_context_hash: approval.approval_context_hash,
      });
      if (!request.isCurrent()) return;
      setDialog(initialDialog);
      await load();
    } catch (error) {
      if (!request.isCurrent()) return;
      await load();
      if (!request.isCurrent()) return;
      setDialog((current) => ({
        ...current,
        approval: current.approval || approval,
        state: isStaleApprovalError(error) ? "stale" : "error",
        error: error.message,
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
