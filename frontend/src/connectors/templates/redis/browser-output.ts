import type { RedisKeyResult } from "./browser-types";

export function readRedisScan(value: unknown): { keys: string[]; nextCursor: string } {
  const output = record(value);
  return {
    keys: Array.isArray(output.keys) ? output.keys.filter((key: unknown): key is string => typeof key === "string") : [],
    nextCursor: typeof output.next_cursor === "string" || typeof output.next_cursor === "number" ? String(output.next_cursor) : "0",
  };
}

export function readRedisKey(value: unknown): RedisKeyResult | null {
  const output = record(value);
  if (typeof output.key !== "string" || typeof output.type !== "string") return null;
  if (output.truncated !== undefined && typeof output.truncated !== "boolean") return null;
  return {
    ...output,
    key: output.key,
    type: output.type,
    ttl_ms: typeof output.ttl_ms === "number" ? output.ttl_ms : undefined,
    truncated: output.truncated,
  };
}

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}
