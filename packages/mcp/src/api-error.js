const safeResultStatuses = new Set(["failed", "blocked", "stale", "declined", "canceled", "error", "outcome_unknown"]);

export function gatewayAPIError(data, httpStatus, retryAfterHeader = null) {
  const error = new Error(data?.error || `AIPermission API request failed with ${httpStatus}`);
  if (typeof data?.code === "string" && data.code.length <= 128) error.code = data.code;
  if (safeResultStatuses.has(data?.status)) error.resultStatus = data.status;
  if (Number.isSafeInteger(data?.request_id) && data.request_id > 0) error.requestID = data.request_id;
  if (typeof data?.assistant_hint === "string" && data.assistant_hint.length <= 2048) error.assistantHint = data.assistant_hint;
  const retryAfterSeconds = safeRetryAfterSeconds(data?.retry_after_seconds, retryAfterHeader);
  if (retryAfterSeconds !== null) error.retryAfterSeconds = retryAfterSeconds;
  return error;
}

function safeRetryAfterSeconds(bodyValue, headerValue) {
  if (Number.isSafeInteger(bodyValue) && bodyValue >= 0 && bodyValue <= 3600) return bodyValue;
  const raw = typeof headerValue === "string" ? headerValue.trim() : "";
  if (!/^\d{1,4}$/.test(raw)) return null;
  const value = Number(raw);
  return value <= 3600 ? value : null;
}
