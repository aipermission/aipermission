import { apiPost } from "../../../lib/api.js";
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
  suppressError = false,
  successMessage,
  post = apiPost,
}) {
  const request = requestGuard.begin(channel || actionName);
  setState({ state: busy, error: "", message: "" });
  try {
    const response = await post("/api/connector-actions/local-run", {
      target_ref: targetRef,
      action_name: actionName,
      input,
      reason,
    });
    if (!request.isCurrent()) return null;
    const item = requireCompletedConnectorAction(response, `${product} action failed.`);
    if (!item) {
      const message = response.display_text || `${product} action is awaiting approval.`;
      setState({ state: "idle", error: "", message });
      void Promise.resolve()
        .then(() => onRefreshActivity?.())
        .catch((refreshError) => {
          if (request.isCurrent()) {
            setState({
              state: "idle",
              error: `Approval is pending, but activity refresh failed: ${actionRunnerErrorMessage(refreshError)}`,
              message,
            });
          }
        });
      return null;
    }
    const message = successMessage ? successMessage(item) : item.display_text || "";
    setState({ state: "idle", error: "", message });
    onCompleted?.(item);
    try {
      await onRefreshActivity?.();
    } catch (refreshError) {
      if (request.isCurrent()) {
        setState({
          state: "idle",
          error: `Action completed, but activity refresh failed: ${actionRunnerErrorMessage(refreshError)}`,
          message,
        });
      }
    }
    return request.isCurrent() ? item : null;
  } catch (error) {
    if (!request.isCurrent()) return null;
    setState(
      suppressError
        ? { state: "idle", error: "", message: "" }
        : { state: "error", error: error.message || `${product} action failed.`, message: "" },
    );
    throw error;
  }
}

function actionRunnerErrorMessage(error) {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error.trim()) return error.trim();
  return "unknown error";
}
