import { useCallback, useRef, useState } from "react";
import { apiGet, apiPost } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
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
        const data = await apiGet("/api/vault-action-approvals?status=approval_pending", { signal: request.signal });
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setApprovals({ state: "ready", data, error: null });
        const pending = data.filter((item) => item.status === "approval_pending");
        setDialog((current) => reconcileVaultApprovalDialog(current, pending, seenPendingRef.current));
      } catch (error) {
        if (!request.isCurrent() || !pollIsCurrent(generation)) return;
        setApprovals({ state: "error", data: [], error: error.message });
      } finally {
        request.complete();
      }
    },
    [pollIsCurrent, requests],
  );

  const run = useCallback(async () => {
    const approval = dialog.approval;
    if (!approval) return;
    const request = requests.begin("decision");
    setDialog((current) => ({ ...current, state: "running", error: null }));
    try {
      await apiPost(`/api/vault-action-approvals/${approval.id}/run`, { user_note: dialog.note });
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
  }, [dialog.approval, dialog.note, load, refreshConsoleSessions, requests]);

  const decline = useCallback(async () => {
    const approval = dialog.approval;
    if (!approval) return;
    const request = requests.begin("decision");
    setDialog((current) => ({ ...current, state: "declining", error: null }));
    try {
      await apiPost(`/api/vault-action-approvals/${approval.id}/decline`, { user_note: dialog.note });
      if (!request.isCurrent()) return;
      setDialog(initialDialog);
      await load();
    } catch (error) {
      if (!request.isCurrent()) return;
      setDialog((current) => ({ ...current, state: "error", error: error.message }));
    } finally {
      request.complete();
    }
  }, [dialog.approval, dialog.note, load, requests]);

  const close = useCallback(() => {
    requests.invalidate("decision");
    setDialog(initialDialog);
  }, [requests]);

  const setNote = useCallback((note) => setDialog((current) => ({ ...current, note })), []);

  return { approvals, close, decline, dialog, load, run, setNote };
}
