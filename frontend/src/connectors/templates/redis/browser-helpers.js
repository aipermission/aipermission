export const defaultRedisPattern = "*";
export const defaultRedisLimit = 100;

export function formatRedisValue(value) {
  if (typeof value === "string") return value;
  return JSON.stringify(value ?? null, null, 2);
}

export function keyMetaText(result) {
  const ttl = Number(result.ttl_ms);
  const ttlText = ttl > 0 ? `${Math.ceil(ttl / 1000)}s TTL` : ttl === -1 ? "persistent" : ttl === -2 ? "missing" : "ttl unknown";
  return `${result.type || "unknown"} · ${ttlText}`;
}

export function redisScanPattern(value) {
  const trimmed = String(value || "").trim();
  if (!trimmed) return defaultRedisPattern;
  if (/[*?[\]]/.test(trimmed)) return trimmed;
  return `*${trimmed}*`;
}

export function uniqueRedisKeys(values) {
  return Array.from(new Set((values || []).filter(Boolean)));
}

export function valueToEditableText(output) {
  if (!output) return "";
  if (output.type !== "string") return formatRedisValue(output.value);
  const text = String(output.value ?? "");
  const trimmed = text.trim();
  if (!trimmed) return text;
  try {
    return JSON.stringify(JSON.parse(trimmed), null, 2);
  } catch {
    return text;
  }
}
