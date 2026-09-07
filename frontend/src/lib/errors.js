export class APIError extends Error {
  constructor(message, { status = 0, code = "", details = null, data = null } = {}) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.code = code;
    this.details = details;
    this.data = data;
    this.kind = classifyAPIError(status);
  }
}

export function classifyAPIError(status) {
  if (status === 401) return "authentication";
  if (status === 403) return "authorization";
  if (status === 404) return "not_found";
  if (status === 409) return "conflict";
  if (status === 429) return "rate_limited";
  if (status === 400 || status === 422) return "validation";
  if ([502, 503, 504].includes(status)) return "unavailable";
  if (status >= 500) return "server";
  return "http";
}

export function errorMessage(error, fallback = "unknown error") {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error.trim()) return error.trim();
  return fallback;
}
