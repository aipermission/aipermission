import { describe, expect, it } from "vitest";
import { readRedisKey, readRedisScan } from "./browser-output";

describe("Redis browser output boundaries", () => {
  it("accepts only string key identities without trimming whitespace", () => {
    expect(readRedisScan({ keys: [" padded ", 1, null, "valid"], next_cursor: 3 })).toEqual({
      keys: [" padded ", "valid"],
      nextCursor: "3",
    });
    expect(readRedisScan(null)).toEqual({ keys: [], nextCursor: "0" });
  });

  it("preserves opaque values and rejects malformed write-sensitive metadata", () => {
    expect(readRedisKey({ key: "key", type: "string", value: " ", truncated: true, ttl_ms: -1 })).toMatchObject({
      key: "key",
      value: " ",
      truncated: true,
      ttl_ms: -1,
    });
    expect(readRedisKey({ key: "key", type: "string", truncated: "true" })).toBeNull();
    expect(readRedisKey({ key: 1, type: "string" })).toBeNull();
    expect(readRedisKey([])).toBeNull();
  });
});
