export const defaultRedisPattern = "*";
export const defaultRedisLimit = 100;

export function formatRedisValue(value: unknown): string {
  if (typeof value === "string") return value;
  return JSON.stringify(value ?? null, null, 2) ?? "null";
}

export function keyMetaText(result: { ttl_ms?: number | string | null; type?: string }): string {
  const ttl = Number(result.ttl_ms);
  const ttlText = ttl > 0 ? `${Math.ceil(ttl / 1000)}s TTL` : ttl === -1 ? "persistent" : ttl === -2 ? "missing" : "ttl unknown";
  return `${result.type || "unknown"} · ${ttlText}`;
}

export function redisScanPattern(value: string | null | undefined): string {
  const trimmed = String(value || "").trim();
  if (!trimmed) return defaultRedisPattern;
  if (/[*?[\]]/.test(trimmed)) return trimmed;
  return `*${trimmed}*`;
}

export function uniqueRedisKeys(values: string[] | null | undefined): string[] {
  return Array.from(new Set((values || []).filter(Boolean)));
}

export function valueToEditableText(output: { type?: string; value?: unknown } | null | undefined): string {
  if (!output) return "";
  if (output.type !== "string") return formatRedisValue(output.value);
  return String(output.value ?? "");
}
