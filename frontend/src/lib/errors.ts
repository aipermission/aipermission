export class APIError extends Error {
  status: number;
  code: string;
  details: unknown;
  data: unknown;
  kind: string;

  constructor(
    message: string,
    { status = 0, code = "", details = null, data = null }: { status?: number; code?: string; details?: unknown; data?: unknown } = {},
  ) {
    super(message);
    this.name = "APIError";
    this.status = status;
    this.code = code;
    this.details = details;
    this.data = data;
    this.kind = classifyAPIError(status);
  }
}

export function classifyAPIError(status: number): string {
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

export function errorMessage(error: unknown, fallback = "unknown error"): string {
  if (error instanceof Error && error.message) return error.message;
  if (typeof error === "string" && error.trim()) return error.trim();
  return fallback;
}
