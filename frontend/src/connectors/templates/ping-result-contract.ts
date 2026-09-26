export type PingResult = {
  ok: boolean;
  received: number;
  sent: number;
  duration_ms: number;
  mode: string;
  message?: string;
  attempts?: { attempt: number; ok: boolean; duration_ms: number; error?: string }[];
};

export function pingResultResponse(value: unknown): PingResult {
  const data = record(value);
  if (
    typeof data.ok !== "boolean" ||
    typeof data.mode !== "string" ||
    !nonnegative(data.received) ||
    !nonnegative(data.sent) ||
    !nonnegative(data.duration_ms) ||
    (data.message !== undefined && typeof data.message !== "string")
  )
    throw invalid();
  if (data.attempts !== undefined) {
    if (!Array.isArray(data.attempts)) throw invalid();
    for (const value of data.attempts) {
      const attempt = record(value);
      if (
        !nonnegative(attempt.attempt) ||
        !nonnegative(attempt.duration_ms) ||
        typeof attempt.ok !== "boolean" ||
        (attempt.error !== undefined && typeof attempt.error !== "string")
      )
        throw invalid();
    }
  }
  return data as PingResult;
}
function nonnegative(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0;
}
function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  return value as Record<string, unknown>;
}
function invalid() {
  return new Error("Invalid host reachability response.");
}
