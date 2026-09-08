import { useCallback, useEffect, useMemo, useState } from "react";
import { apiGet } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";

const idleAction = { state: "idle", error: null };

export function useConnectorApprovalDialog({ approvals, selectedTargetRef, runApproval, declineApproval }) {
  const [activeID, setActiveID] = useState(null);
  const [snapshot, setSnapshot] = useState(null);
  const [dismissedIDs, setDismissedIDs] = useState({});
  const [note, setNote] = useState("");
  const [action, setAction] = useState(idleAction);
  const requests = useRequestGuard(`console-approval:${selectedTargetRef || "none"}`);

  const pendingApprovals = useMemo(() => (approvals || []).filter((approval) => approval.status === "approval_pending"), [approvals]);
  const selectedPendingApprovals = useMemo(
    () => (selectedTargetRef ? pendingApprovals.filter((approval) => approval.target_ref === selectedTargetRef) : []),
    [pendingApprovals, selectedTargetRef],
  );
  const activeApproval = snapshot && Number(snapshot.id) === Number(activeID) ? snapshot : null;

  const reset = useCallback(() => {
    requests.invalidate("detail");
    requests.invalidate("mutation");
    setActiveID(null);
    setSnapshot(null);
    setNote("");
    setAction(idleAction);
  }, [requests]);

  const open = useCallback(
    async (approval) => {
      const request = requests.begin("detail");
      setActiveID(approval.id);
      setSnapshot({ ...approval, preview: {}, input: {} });
      setNote("");
      setAction({ state: "loading", error: null });
      try {
        const exact = await apiGet(`/api/connector-action-approvals/${approval.id}`, { signal: request.signal });
        if (!request.isCurrent()) return;
        setSnapshot(exact);
        setAction(
          exact.status === "approval_pending"
            ? idleAction
            : { state: "failed", error: "This connector approval is no longer pending. Refresh activity before taking another action." },
        );
      } catch (error) {
        if (!request.isCurrent()) return;
        setAction({ state: "load_error", error: error.message });
      } finally {
        request.complete();
      }
    },
    [requests],
  );

  const close = useCallback(() => {
    if (activeID) setDismissedIDs((current) => ({ ...current, [activeID]: true }));
    reset();
  }, [activeID, reset]);

  const approve = useCallback(async () => {
    if (!activeApproval) return;
    const approval = activeApproval;
    const request = requests.begin("mutation");
    setAction({ state: "running", error: null });
    try {
      const item = await runApproval(approval.id, note);
      if (!request.isCurrent()) return;
      if (["error", "failed", "stale"].includes(item?.status)) {
        setSnapshot({ ...approval, ...item });
        setAction({ state: item.status === "stale" ? "stale" : "failed", error: item.error || "Connector action failed." });
        return;
      }
      setDismissedIDs((current) => withoutKey(current, approval.id));
      reset();
    } catch (error) {
      if (!request.isCurrent()) return;
      setSnapshot(approval);
      setAction({ state: isStaleApprovalError(error) ? "stale" : "error", error: error.message });
    } finally {
      request.complete();
    }
  }, [activeApproval, note, requests, reset, runApproval]);

  const decline = useCallback(async () => {
    if (!activeApproval) return;
    const approval = activeApproval;
    const request = requests.begin("mutation");
    setAction({ state: "declining", error: null });
    try {
      await declineApproval(approval.id, note);
      if (!request.isCurrent()) return;
      setDismissedIDs((current) => withoutKey(current, approval.id));
      reset();
    } catch (error) {
      if (!request.isCurrent()) return;
      setAction({ state: "error", error: error.message });
    } finally {
      request.complete();
    }
  }, [activeApproval, declineApproval, note, requests, reset]);

  useEffect(() => reset(), [reset, selectedTargetRef]);

  useEffect(() => {
    if (
      activeID &&
      !pendingApprovals.some((approval) => Number(approval.id) === Number(activeID)) &&
      !isTerminalActionState(action.state)
    ) {
      reset();
      return;
    }
    if (activeID || selectedPendingApprovals.length === 0) return;
    const next = selectedPendingApprovals.find((approval) => !dismissedIDs[approval.id]);
    if (next) void open(next);
  }, [action.state, activeID, dismissedIDs, open, pendingApprovals, reset, selectedPendingApprovals]);

  return { action, activeApproval, approve, close, decline, note, open, pendingApprovals, selectedPendingApprovals, setNote };
}

function isStaleApprovalError(error) {
  const message = String(error?.message || "").toLowerCase();
  return message.includes("stale") || message.includes("approval context") || message.includes("fresh request");
}

function isTerminalActionState(state) {
  return ["error", "failed", "running", "stale"].includes(state);
}

function withoutKey(value, key) {
  const next = { ...value };
  delete next[key];
  return next;
}
