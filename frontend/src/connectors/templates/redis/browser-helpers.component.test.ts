import { describe, expect, it } from "vitest";
import { formatRedisValue, keyMetaText, redisScanPattern, uniqueRedisKeys, valueToEditableText } from "./browser-helpers";

describe("Redis browser value conversion", () => {
  it("preserves strings and formats structured, empty, and non-JSON values predictably", () => {
    expect(formatRedisValue('{"enabled":true}')).toBe('{"enabled":true}');
    expect(formatRedisValue({ enabled: true })).toBe('{\n  "enabled": true\n}');
    expect(formatRedisValue(null)).toBe("null");
    expect(formatRedisValue(undefined)).toBe("null");
    expect(formatRedisValue(Symbol("unsupported"))).toBe("null");
    expect(formatRedisValue(false)).toBe("false");
  });

  it("distinguishes persistent, missing, positive, and unknown TTL states without rounding down", () => {
    expect(keyMetaText({ type: "string", ttl_ms: 1001 })).toBe("string · 2s TTL");
    expect(keyMetaText({ type: "hash", ttl_ms: "-1" })).toBe("hash · persistent");
    expect(keyMetaText({ type: "none", ttl_ms: -2 })).toBe("none · missing");
    expect(keyMetaText({ ttl_ms: 0 })).toBe("unknown · ttl unknown");
    expect(keyMetaText({ ttl_ms: "not-a-number" })).toBe("unknown · ttl unknown");
    expect(keyMetaText({})).toBe("unknown · ttl unknown");
  });

  it("keeps string editing distinct from structured viewing and handles cleared selections", () => {
    expect(valueToEditableText(null)).toBe("");
    expect(valueToEditableText(undefined)).toBe("");
    expect(valueToEditableText({ type: "string", value: "0" })).toBe("0");
    expect(valueToEditableText({ type: "string", value: 0 })).toBe("0");
    expect(valueToEditableText({ type: "string" })).toBe("");
    expect(valueToEditableText({ type: "hash", value: { count: 0 } })).toBe('{\n  "count": 0\n}');
  });
});

describe("Redis key scan input", () => {
  it("treats plain search as a substring while preserving explicit Redis glob patterns", () => {
    expect(redisScanPattern(null)).toBe("*");
    expect(redisScanPattern(undefined)).toBe("*");
    expect(redisScanPattern("   ")).toBe("*");
    expect(redisScanPattern(" heartbeat ")).toBe("*heartbeat*");
    expect(redisScanPattern(" cache:* ")).toBe("cache:*");
    expect(redisScanPattern("job:?")).toBe("job:?");
    expect(redisScanPattern("job:[12]")).toBe("job:[12]");
  });

  it("deduplicates scans without losing the first occurrence order or retaining empty keys", () => {
    const keys = ["cache:b", "cache:a", "cache:b", "", "cache:c"];
    expect(uniqueRedisKeys(keys)).toEqual(["cache:b", "cache:a", "cache:c"]);
    expect(keys).toEqual(["cache:b", "cache:a", "cache:b", "", "cache:c"]);
    expect(uniqueRedisKeys(null)).toEqual([]);
    expect(uniqueRedisKeys(undefined)).toEqual([]);
  });
});
