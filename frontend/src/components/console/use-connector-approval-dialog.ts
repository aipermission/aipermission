import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiGet } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
import { connectorApproval } from "../../lib/gateway-contracts/security-contracts";
import type { ConnectorApproval } from "../../lib/gateway-contracts/security-contracts";
import { APIError, errorMessage } from "../../lib/errors.ts";
import type { useGatewayActivityResources } from "../use-gateway-activity-resources.ts";

type Activity = ReturnType<typeof useGatewayActivityResources>;
type Props = {
  approvals: ConnectorApproval[];
  selectedTargetRef: string;
  runApproval: Activity["runConnectorActionApproval"];
  declineApproval: Activity["declineConnectorActionApproval"];
};
type ApprovalSnapshot = ConnectorApproval & { request_id?: number };
export type ApprovalDialogAction = {
  state: "idle" | "loading" | "failed" | "load_error" | "running" | "stale" | "error" | "declining";
  error: string | null;
};
const idleAction: ApprovalDialogAction = { state: "idle", error: null };

export function useConnectorApprovalDialog({ approvals, selectedTargetRef, runApproval, declineApproval }: Props) {
  const [activeID, setActiveID] = useState<number | null>(null);
  const [snapshot, setSnapshot] = useState<ApprovalSnapshot | null>(null);
  const [detailVerified, setDetailVerified] = useState(false);
  const [dismissedIDs, setDismissedIDs] = useState<Record<number, boolean>>({});
  const [note, setNote] = useState("");
  const [action, setAction] = useState(idleAction);
  const requests = useRequestGuard(`console-approval:${selectedTargetRef || "none"}`);
  const selectedTargetRefRef = useRef(selectedTargetRef);
  selectedTargetRefRef.current = selectedTargetRef;

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
    setDetailVerified(false);
    setNote("");
    setAction(idleAction);
  }, [requests]);

  const open = useCallback(
    async (approval: ConnectorApproval) => {
      requests.invalidate("mutation");
      const request = requests.begin("detail");
      setActiveID(approval.id);
      setSnapshot({ ...approval, preview: {}, input: {} });
      setDetailVerified(false);
      setNote("");
      setAction({ state: "loading", error: null });
      try {
        const exact = await readExactApproval(approval, selectedTargetRefRef.current, request.signal);
        if (!request.isCurrent()) return;
        setSnapshot(exact);
        setDetailVerified(true);
        setAction(
          exact.status === "approval_pending"
            ? idleAction
            : { state: "failed", error: "This connector approval is no longer pending. Refresh activity before taking another action." },
        );
      } catch (error) {
        if (!request.isCurrent()) return;
        setAction({ state: "load_error", error: errorMessage(error, "Could not load the approval.") });
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
    if (!activeApproval || !detailVerified) return;
    const approval = activeApproval;
    const request = requests.begin("mutation");
    setAction({ state: "running", error: null });
    try {
      const item = await runApproval(approval, note);
      if (!request.isCurrent()) return;
      if (!["completed", "running"].includes(item?.status)) {
        setSnapshot({ ...approval, ...item });
        setAction({ state: item.status === "stale" ? "stale" : "failed", error: item.error || "Connector action failed." });
        return;
      }
      setDismissedIDs((current) => withoutKey(current, approval.id));
      reset();
    } catch (error) {
      if (!request.isCurrent()) return;
      const uncertain = connectorOutcomeUnknown(error, approval);
      if (uncertain) {
        setSnapshot({ ...approval, ...uncertain });
        setAction({ state: "failed", error: uncertain.assistant_hint || uncertain.error || errorMessage(error) });
        return;
      }
      let exact: ConnectorApproval | null = null;
      try {
        exact = await readExactApproval(approval, approval.target_ref, request.signal);
      } catch {
        // A lost run response is unsafe to retry until its exact request can be reconciled.
      }
      if (!request.isCurrent()) return;
      setSnapshot(exact || approval);
      if (exact && exact.status !== "approval_pending") {
        setAction({
          state: "stale",
          error: `${errorMessage(error)} This connector approval is ${exact.status}; the run response may have been lost.`,
        });
        return;
      }
      if (!exact) {
        setAction({ state: "failed", error: `${errorMessage(error)} The request outcome could not be reconciled; do not retry yet.` });
        return;
      }
      setAction({ state: isStaleApprovalError(error) ? "stale" : "error", error: errorMessage(error) });
    } finally {
      request.complete();
    }
  }, [activeApproval, detailVerified, note, requests, reset, runApproval]);

  const decline = useCallback(async () => {
    if (!activeApproval || !detailVerified) return;
    const approval = activeApproval;
    const request = requests.begin("mutation");
    setAction({ state: "declining", error: null });
    try {
      await declineApproval(approval, note);
      if (!request.isCurrent()) return;
      setDismissedIDs((current) => withoutKey(current, approval.id));
      reset();
    } catch (error) {
      if (!request.isCurrent()) return;
      let exact: ConnectorApproval | null = null;
      try {
        exact = await readExactApproval(approval, approval.target_ref, request.signal);
      } catch {
        // Preserve the reviewed snapshot and note when reconciliation is unavailable.
      }
      if (!request.isCurrent()) return;
      setSnapshot(exact || approval);
      if (exact && exact.status !== "approval_pending") {
        setAction({
          state: "stale",
          error: `${errorMessage(error)} This connector approval is no longer pending; the decline may already have been recorded.`,
        });
        return;
      }
      setAction({ state: isStaleApprovalError(error) ? "stale" : "error", error: errorMessage(error) });
    } finally {
      request.complete();
    }
  }, [activeApproval, declineApproval, detailVerified, note, requests, reset]);

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

async function readExactApproval(approval: ConnectorApproval, targetRef: string, signal: AbortSignal) {
  return connectorApproval(await apiGet(`/api/connector-action-approvals/${approval.id}`, { signal }), {
    id: approval.id,
    targetRef,
    actionName: approval.action_name,
  });
}

function isStaleApprovalError(error: unknown) {
  return error instanceof APIError && ["approval_context_changed", "approval_not_pending"].includes(error.code);
}

function connectorOutcomeUnknown(error: unknown, approval: ConnectorApproval) {
  const data = error instanceof APIError ? error.data : null;
  if (!data || typeof data !== "object" || Array.isArray(data)) return null;
  if (!("status" in data) || !("request_id" in data) || !("error" in data) || !("assistant_hint" in data)) return null;
  if (
    data?.status !== "outcome_unknown" ||
    typeof data.request_id !== "number" || !Number.isSafeInteger(data.request_id) ||
    Number(data.request_id) !== Number(approval.id) ||
    typeof data.error !== "string" ||
    typeof data.assistant_hint !== "string"
  ) {
    return null;
  }
  return { status: "outcome_unknown" as const, request_id: data.request_id, error: data.error, assistant_hint: data.assistant_hint };
}

function isTerminalActionState(state: ApprovalDialogAction["state"]) {
  return ["declining", "error", "failed", "load_error", "running", "stale"].includes(state);
}

function withoutKey(value: Record<number, boolean>, key: number) {
  const next = { ...value };
  delete next[key];
  return next;
}
