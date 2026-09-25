import assert from "node:assert/strict";
import test from "node:test";
import { keyMetaText, redisScanPattern, uniqueRedisKeys, valueToEditableText } from "./browser-helpers.ts";

test("Redis string drafts preserve exact JSON number and whitespace text", () => {
  const values = [
    '{"id":9007199254740993,"enabled":true}',
    '{"id":-9007199254740993,"amount":1.0000000000000001}',
    '{ "exponent": 1e+30, "quoted": "9007199254740993" }',
    "plain text",
  ];

  for (const value of values) assert.equal(valueToEditableText({ type: "string", value }), value);
});

test("Redis browse helpers preserve explicit globs and stable key order", () => {
  assert.equal(redisScanPattern("user"), "*user*");
  assert.equal(redisScanPattern("user:*"), "user:*");
  assert.deepEqual(uniqueRedisKeys(["first", "second", "first", ""]), ["first", "second"]);
  assert.equal(keyMetaText({ type: "hash", ttl_ms: 1500 }), "hash · 2s TTL");
});
