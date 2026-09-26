import { apiPost } from "../../../lib/api.ts";
import { errorMessage } from "../../../lib/errors.ts";
import { connectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { requireCompletedConnectorAction } from "./action-result.ts";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { createRequestGuard } from "../../../lib/request-guard";

export type ConnectorActionState = { state: string; error: string; message: string };
export type GuardedConnectorActionOptions = {
  requestGuard: ReturnType<typeof createRequestGuard>;
  channel?: string;
  targetRef: string;
  actionName: string;
  input?: Record<string, unknown>;
  reason: string;
  busy?: string;
  product: string;
  setState: (_state: ConnectorActionState) => void;
  onRefreshActivity?: (() => unknown) | null;
  onCompleted?: ((_item: ConnectorActionResponse) => void) | null;
  onPending?: ((_item: ConnectorActionResponse) => void) | null;
  suppressError?: boolean;
  successMessage?: ((_item: ConnectorActionResponse) => string) | null;
  post?: (_path: string, _payload: Record<string, unknown>, _options: { signal: AbortSignal }) => Promise<unknown>;
};
type ActionFeedback = Pick<GuardedConnectorActionOptions, "setState" | "onRefreshActivity"> & { canUpdateState: () => boolean };

export async function runGuardedConnectorAction({
  requestGuard,
  channel,
  targetRef,
  actionName,
  input = {},
  reason,
  busy = "running",
  product,
  setState,
  onRefreshActivity,
  onCompleted = null,
  onPending = null,
  suppressError = false,
  successMessage = null,
  post = apiPost,
}: GuardedConnectorActionOptions): Promise<ConnectorActionResponse | null> {
  const request = requestGuard.begin(channel || actionName);
  const visibility = requestGuard.claimVisibility();
  const canUpdateState = () => request.isCurrent() && visibility.isCurrent();
  setState({ state: busy, error: "", message: "" });
  try {
    const response = connectorActionResponse(
      await post(
        "/api/connector-actions/local-run",
        {
          target_ref: targetRef,
          action_name: actionName,
          input,
          reason,
        },
        { signal: request.signal },
      ),
      { targetRef, actionName },
    );
    if (!request.isCurrent()) return null;
    const item = requireCompletedConnectorAction(response, `${product} action failed.`);
    if (!item) return handlePendingAction({ response, product, canUpdateState, setState, onRefreshActivity, onPending });
    return await handleCompletedAction({ item, request, canUpdateState, setState, onRefreshActivity, onCompleted, successMessage });
  } catch (error) {
    if (!request.isCurrent()) return null;
    if (outcomeUnknown(error)) await handleUnknownOutcome({ error, product, canUpdateState, setState, onRefreshActivity });
    if (canUpdateState()) {
      setState(
        suppressError
          ? { state: "idle", error: "", message: "" }
          : { state: "error", error: errorMessage(error, `${product} action failed.`), message: "" },
      );
    }
    throw error;
  } finally {
    request.complete();
  }
}

function handlePendingAction({
  response,
  product,
  canUpdateState,
  setState,
  onRefreshActivity,
  onPending,
}: ActionFeedback & Pick<GuardedConnectorActionOptions, "product" | "onPending"> & { response: ConnectorActionResponse }) {
  const message = response.display_text || `${product} action is awaiting approval.`;
  onPending?.(response);
  if (canUpdateState()) setState({ state: "idle", error: "", message });
  void Promise.resolve()
    .then(() => onRefreshActivity?.())
    .catch((refreshError) => {
      if (!canUpdateState()) return;
      setState({
        state: "idle",
        error: `Approval is pending, but activity refresh failed: ${errorMessage(refreshError)}`,
        message,
      });
    });
  return null;
}

async function handleCompletedAction({
  item,
  request,
  canUpdateState,
  setState,
  onRefreshActivity,
  onCompleted,
  successMessage,
}: ActionFeedback &
  Pick<GuardedConnectorActionOptions, "onCompleted" | "successMessage"> & {
    item: ConnectorActionResponse;
    request: ReturnType<GuardedConnectorActionOptions["requestGuard"]["begin"]>;
  }) {
  const message = successMessage ? successMessage(item) : item.display_text || "";
  if (canUpdateState()) setState({ state: "idle", error: "", message });
  onCompleted?.(item);
  try {
    await onRefreshActivity?.();
  } catch (refreshError) {
    if (canUpdateState()) {
      setState({ state: "idle", error: `Action completed, but activity refresh failed: ${errorMessage(refreshError)}`, message });
    }
  }
  return request.isCurrent() ? item : null;
}

function outcomeUnknown(error: unknown): Record<string, unknown> | null {
  if (!error || typeof error !== "object") return null;
  const actionItem: unknown = "actionItem" in error ? error.actionItem : null;
  if (actionItem && typeof actionItem === "object" && "status" in actionItem && actionItem.status === "outcome_unknown")
    return Object.fromEntries(Object.entries(actionItem));
  const data: unknown = "data" in error ? error.data : null;
  return data && typeof data === "object" && "status" in data && data.status === "outcome_unknown"
    ? Object.fromEntries(Object.entries(data))
    : null;
}

async function handleUnknownOutcome({
  error,
  product,
  canUpdateState,
  setState,
  onRefreshActivity,
}: ActionFeedback & { error: unknown; product: string }) {
  const uncertain = outcomeUnknown(error);
  if (!uncertain) throw error;
  let message =
    typeof uncertain.error === "string" && uncertain.error ? uncertain.error : errorMessage(error, `${product} action outcome is unknown.`);
  if (uncertain.request_id) message += ` Request ${uncertain.request_id}.`;
  if (typeof uncertain.assistant_hint === "string" && uncertain.assistant_hint) message += ` ${uncertain.assistant_hint}`;
  try {
    await onRefreshActivity?.();
  } catch (refreshError) {
    message += ` Activity refresh failed: ${errorMessage(refreshError)}`;
  }
  if (canUpdateState()) setState({ state: "error", error: message, message: "" });
  throw error;
}
