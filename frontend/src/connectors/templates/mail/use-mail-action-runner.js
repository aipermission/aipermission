import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiPost } from "../../../lib/api";
import { connectorActionCode, connectorActionError, connectorActionPending } from "../_shared/action-result";
import { mailActionResolution, mailActionSummary } from "./helpers";

const browserActions = new Set(["list_folders", "search_messages", "get_message"]);

export function useMailActionRunner({ target, approvals, scopeKey, onRefreshActivity, onResolution }) {
  const [state, setState] = useState({ state: "idle", error: "", message: "" });
  const [pendingActions, setPendingActions] = useState({});
  const [resultDialog, setResultDialog] = useState({ open: false, actionName: "", summary: "", item: null });
  const requestGeneration = useRef(0);
  const currentScope = useRef(scopeKey);
  const resolveForEffect = useEffectEvent((pending, resolution) => onResolution?.(pending, resolution));
  const reconcileForEffect = useEffectEvent(async (pending, resolution) => {
    const { actionName, generation } = pending;
    const { item } = resolution;
    if (browserActions.has(actionName) && generation !== requestGeneration.current) return;
    if (resolution.state !== "completed") {
      const fallback = `${String(actionName || "Mail action").replaceAll("_", " ")} was not approved or could not be completed.`;
      const summary = item.error || item.display_text || fallback;
      setState({ state: "error", error: connectorActionError(item, fallback), message: "", result: { actionName, summary, item } });
      await resolveForEffect(pending, resolution);
      return;
    }
    const summary = mailActionSummary(actionName, item);
    setState({ state: "idle", error: "", message: summary, result: { actionName, summary, item } });
    await resolveForEffect(pending, resolution);
  });
  const activeItems = useMemo(
    () => (approvals?.data || []).filter((item) => item.target_ref === target.ref),
    [approvals?.data, target.ref],
  );

  useEffect(() => {
    requestGeneration.current += 1;
    currentScope.current = scopeKey;
    setState({ state: "idle", error: "", message: "" });
    setPendingActions({});
    setResultDialog({ open: false, actionName: "", summary: "", item: null });
  }, [scopeKey]);

  useEffect(() => {
    const resolved = Object.values(pendingActions)
      .map((pending) => ({ pending, resolution: mailActionResolution(activeItems, pending.requestID) }))
      .filter(({ resolution }) => resolution && resolution.state !== "pending");
    if (resolved.length === 0) return;
    setPendingActions((current) => {
      const next = { ...current };
      for (const { pending } of resolved) delete next[pending.requestID];
      return next;
    });
    for (const { pending, resolution } of resolved) void reconcileForEffect(pending, resolution);
  }, [activeItems, pendingActions]);

  function reportActivityRefreshFailure() {
    setState((current) =>
      current.state === "idle" ? { ...current, error: "Activity refresh unavailable.", message: "", result: null } : current,
    );
  }

  function refreshActivitySafely() {
    try {
      const refresh = onRefreshActivity?.();
      void Promise.resolve(refresh).catch(reportActivityRefreshFailure);
    } catch {
      reportActivityRefreshFailure();
    }
  }

  async function runMailAction(actionName, input, reason, busyState = "running", pendingContext = {}) {
    const generation = ++requestGeneration.current;
    const actionScope = scopeKey;
    setState({ state: busyState, error: "", message: "" });
    try {
      const item = await apiPost("/api/connector-actions/local-run", {
        target_ref: target.ref,
        action_name: actionName,
        input,
        reason,
      });
      if (generation !== requestGeneration.current || actionScope !== currentScope.current) return null;
      const actionError = connectorActionError(item);
      if (actionError) throw actionFailure(actionName, actionError, item, setState, setResultDialog);
      if (connectorActionPending(item)) {
        setPendingActions((current) => ({
          ...current,
          [item.id]: { requestID: item.id, actionName, context: pendingContext, generation },
        }));
        const message = item.display_text || "Mail action is awaiting approval.";
        setState({ state: "idle", error: "", message, result: { actionName, summary: message, item } });
        refreshActivitySafely();
        return item;
      }
      const summary = mailActionSummary(actionName, item);
      setState({ state: "idle", error: "", message: summary, result: { actionName, summary, item } });
      refreshActivitySafely();
      return item;
    } catch (error) {
      if (generation === requestGeneration.current && actionScope === currentScope.current) {
        const message = error.message || "Mail action failed.";
        setState({
          state: "error",
          error: message,
          message: "",
          result: error.actionResult || { actionName, summary: message, item: null },
        });
      }
      throw error;
    }
  }

  return {
    state,
    setState,
    pendingActions,
    activeItems,
    latestAction: activeItems[0] || null,
    resultDialog,
    openResultDialog: (result) => setResultDialog({ open: true, ...result }),
    closeResultDialog: () => setResultDialog({ open: false, actionName: "", summary: "", item: null }),
    runMailAction,
  };
}

function actionFailure(actionName, message, item, setState, setResultDialog) {
  const result = { actionName, summary: message, item };
  setState({ state: "error", error: message, message: "", result });
  if (item?.output) setResultDialog({ open: true, ...result });
  const failure = new Error(message);
  failure.actionItem = item;
  failure.actionResult = result;
  return failure;
}

export function isStaleMessageFailure(resolution) {
  return resolution.state !== "completed" && connectorActionCode(resolution.item) === "stale_message_reference";
}
