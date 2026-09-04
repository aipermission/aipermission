const safeResultStatuses = new Set(["failed", "blocked", "stale", "declined", "canceled", "error", "outcome_unknown"]);

export function gatewayAPIError(data, httpStatus) {
  const error = new Error(data?.error || `AIPermission API request failed with ${httpStatus}`);
  if (typeof data?.code === "string" && data.code.length <= 128) error.code = data.code;
  if (safeResultStatuses.has(data?.status)) error.resultStatus = data.status;
  if (Number.isSafeInteger(data?.request_id) && data.request_id > 0) error.requestID = data.request_id;
  if (typeof data?.assistant_hint === "string" && data.assistant_hint.length <= 2048) error.assistantHint = data.assistant_hint;
  if (Number.isSafeInteger(data?.retry_after_seconds) && data.retry_after_seconds >= 0 && data.retry_after_seconds <= 3600) {
    error.retryAfterSeconds = data.retry_after_seconds;
  }
  return error;
}
