import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiPost } from "../../../lib/api";
import { useRequestGuard } from "../../../lib/request-guard";
import { connectorActionCode, connectorActionError, connectorActionPending, connectorActionRequestID } from "../_shared/action-result";
import { mailActionResolution, mailActionSummary } from "./helpers";
import { errorMessage } from "../../../lib/errors";
import { connectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { useConnectorMutationOwnership } from "../_shared/use-connector-mutation-ownership";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { Dispatch, SetStateAction } from "react";
import type { createRequestGuard } from "../../../lib/request-guard";
import type { MailActionResult } from "./action-result-dialog";
import type { MailActionItem, MailActionResolution, MailPendingAction, MailPendingContext, MailRunnerState } from "./action-types";

interface MailActionRunnerProps {
  target: { ref: string };
  approvals?: { data: MailActionItem[]; state?: string } | null;
  scopeKey: string;
  onRefreshActivity?: () => unknown;
  onResolution?: (_pending: MailPendingAction, _resolution: MailActionResolution, _ownsRunner: boolean) => unknown;
}
type Request = ReturnType<ReturnType<typeof createRequestGuard>["begin"]>;

const browserActions = new Set(["list_folders", "search_messages", "get_message"]);
const outboundActions = ["send_message", "reply_message"];

export function useMailActionRunner({ target, approvals, scopeKey, onRefreshActivity, onResolution }: MailActionRunnerProps) {
  const [state, setState] = useState<MailRunnerState>({ state: "idle", error: "", message: "" });
  const [pendingActions, setPendingActions] = useState<Record<number, MailPendingAction>>({});
  const [resultDialog, setResultDialog] = useState<MailActionResult & { open: boolean }>({
    open: false,
    actionName: "",
    summary: "",
    item: null,
  });
  const requestGeneration = useRef(0);
  const currentScope = useRef(scopeKey);
  const requests = useRequestGuard(`mail-actions:${scopeKey}`);
  const outboundOwner = useConnectorMutationOwnership(target.ref, outboundActions, approvals?.state);
  const resolveForEffect = useEffectEvent((pending: MailPendingAction, resolution: MailActionResolution, ownsRunner: boolean) =>
    onResolution?.(pending, resolution, ownsRunner),
  );
  const reconcileForEffect = useEffectEvent(async (pending: MailPendingAction, resolution: MailActionResolution) => {
    const { actionName, generation, scope } = pending;
    const { item } = resolution;
    if (scope !== currentScope.current) return;
    const ownsRunner = generation === requestGeneration.current;
    if (browserActions.has(actionName) && !ownsRunner) return;
    // Mutations still reconcile offscreen, but only the latest action owns status.
    if (!ownsRunner) {
      await resolveForEffect(pending, resolution, ownsRunner);
      return;
    }
    if (resolution.state !== "completed") {
      const fallback = `${String(actionName || "Mail action").replaceAll("_", " ")} was not approved or could not be completed.`;
      const summary = item.error || item.display_text || fallback;
      setState({ state: "error", error: connectorActionError(item, fallback), message: "", result: { actionName, summary, item } });
      await resolveForEffect(pending, resolution, ownsRunner);
      return;
    }
    const summary = mailActionSummary(actionName, item);
    setState({ state: "idle", error: "", message: summary, result: { actionName, summary, item } });
    await resolveForEffect(pending, resolution, ownsRunner);
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
      .filter((value): value is { pending: MailPendingAction; resolution: MailActionResolution } =>
        Boolean(value.resolution && value.resolution.state !== "pending"),
      );
    if (resolved.length === 0) return;
    setPendingActions((current) => {
      const next = { ...current };
      for (const { pending } of resolved) delete next[pending.requestID];
      return next;
    });
    for (const { pending, resolution } of resolved) void reconcileForEffect(pending, resolution);
  }, [activeItems, pendingActions]);

  function reportActivityRefreshFailure(request: Request, generation: number, actionScope: string) {
    if (!request.isCurrent() || generation !== requestGeneration.current || actionScope !== currentScope.current) return;
    setState((current) =>
      current.state === "idle" ? { ...current, error: "Activity refresh unavailable.", message: "", result: null } : current,
    );
  }

  function refreshActivitySafely(request: Request, generation: number, actionScope: string) {
    try {
      const refresh = onRefreshActivity?.();
      void Promise.resolve(refresh).catch(() => reportActivityRefreshFailure(request, generation, actionScope));
    } catch {
      reportActivityRefreshFailure(request, generation, actionScope);
    }
  }

  async function executeMailAction(
    actionName: string,
    input: Record<string, unknown>,
    reason: string,
    busyState = "running",
    pendingContext: MailPendingContext = {},
    onPending?: (_item: ConnectorActionResponse) => void,
  ) {
    const generation = ++requestGeneration.current;
    const actionScope = scopeKey;
    const request = requests.begin("action");
    setState({ state: busyState, error: "", message: "" });
    try {
      const response: unknown = await apiPost(
        "/api/connector-actions/local-run",
        {
          target_ref: target.ref,
          action_name: actionName,
          input,
          reason,
        },
        { signal: request.signal, ...(outboundActions.includes(actionName) ? { exclusiveMutationActions: outboundActions } : {}) },
      );
      if (!request.isCurrent() || generation !== requestGeneration.current || actionScope !== currentScope.current) return null;
      const item = connectorActionResponse(response, { targetRef: target.ref, actionName });
      const actionError = connectorActionError(item);
      if (actionError) throw actionFailure(actionName, actionError, item, setState, setResultDialog);
      if (connectorActionPending(item)) {
        onPending?.(item);
        const requestID = connectorActionRequestID(item);
        setPendingActions((current) => ({
          ...current,
          [requestID]: { requestID, actionName, context: pendingContext, generation, scope: actionScope },
        }));
        const message = item.display_text || "Mail action is awaiting approval.";
        setState({ state: "idle", error: "", message, result: { actionName, summary: message, item } });
        refreshActivitySafely(request, generation, actionScope);
        return item;
      }
      const summary = mailActionSummary(actionName, item);
      setState({ state: "idle", error: "", message: summary, result: { actionName, summary, item } });
      refreshActivitySafely(request, generation, actionScope);
      return item;
    } catch (error) {
      if (!request.isCurrent() || generation !== requestGeneration.current || actionScope !== currentScope.current) return null;
      const message = errorMessage(error, "Mail action failed.");
      setState({
        state: "error",
        error: message,
        message: "",
        result: error instanceof MailActionFailure ? error.actionResult : { actionName, summary: message, item: null },
      });
      throw error;
    } finally {
      request.complete();
    }
  }

  function runMailAction(
    actionName: string,
    input: Record<string, unknown>,
    reason: string,
    busyState = "running",
    context: MailPendingContext = {},
  ) {
    const execute = (onPending?: (_item: ConnectorActionResponse) => void) =>
      executeMailAction(actionName, input, reason, busyState, context, onPending);
    return outboundActions.includes(actionName) ? outboundOwner.run(execute) : execute();
  }

  return {
    state,
    setState,
    pendingActions,
    activeItems,
    latestAction: activeItems[0] || null,
    resultDialog,
    openResultDialog: (result: MailActionResult) => setResultDialog({ open: true, ...result }),
    closeResultDialog: () => setResultDialog({ open: false, actionName: "", summary: "", item: null }),
    runMailAction,
    outboundLocked: outboundOwner.locked,
  };
}

export class MailActionFailure extends Error {
  readonly actionItem: MailActionItem;
  readonly actionResult: MailActionResult;
  constructor(message: string, actionItem: MailActionItem, actionResult: MailActionResult) {
    super(message);
    this.actionItem = actionItem;
    this.actionResult = actionResult;
  }
}

function actionFailure(
  actionName: string,
  message: string,
  item: MailActionItem,
  setState: Dispatch<SetStateAction<MailRunnerState>>,
  setResultDialog: Dispatch<SetStateAction<MailActionResult & { open: boolean }>>,
) {
  const result = { actionName, summary: message, item };
  setState({ state: "error", error: message, message: "", result });
  if (item?.output) setResultDialog({ open: true, ...result });
  return new MailActionFailure(message, item, result);
}

export function isStaleMessageFailure(resolution: MailActionResolution) {
  return resolution.state !== "completed" && connectorActionCode(resolution.item) === "stale_message_reference";
}
