import { apiPost } from "../../../lib/api.js";
import { errorMessage } from "../../../lib/errors.js";
import { requireCompletedConnectorAction } from "./action-result.js";

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
  onCompleted,
  onPending,
  suppressError = false,
  successMessage,
  post = apiPost,
}) {
  const request = requestGuard.begin(channel || actionName);
  const visibility = requestGuard.claimVisibility();
  const canUpdateState = () => request.isCurrent() && visibility.isCurrent();
  setState({ state: busy, error: "", message: "" });
  try {
    const response = await post(
      "/api/connector-actions/local-run",
      {
        target_ref: targetRef,
        action_name: actionName,
        input,
        reason,
      },
      { signal: request.signal },
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

function handlePendingAction({ response, product, canUpdateState, setState, onRefreshActivity, onPending }) {
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

async function handleCompletedAction({ item, request, canUpdateState, setState, onRefreshActivity, onCompleted, successMessage }) {
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

function outcomeUnknown(error) {
  if (error?.actionItem?.status === "outcome_unknown") return error.actionItem;
  return error?.data?.status === "outcome_unknown" ? error.data : null;
}

async function handleUnknownOutcome({ error, product, canUpdateState, setState, onRefreshActivity }) {
  const uncertain = outcomeUnknown(error);
  let message = uncertain.error || errorMessage(error, `${product} action outcome is unknown.`);
  if (uncertain.request_id) message += ` Request ${uncertain.request_id}.`;
  if (uncertain.assistant_hint) message += ` ${uncertain.assistant_hint}`;
  try {
    await onRefreshActivity?.();
  } catch (refreshError) {
    message += ` Activity refresh failed: ${errorMessage(refreshError)}`;
  }
  if (canUpdateState()) setState({ state: "error", error: message, message: "" });
  throw error;
}
