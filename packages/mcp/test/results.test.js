import assert from "node:assert/strict";
import test from "node:test";

import { errorResult, jsonToolResult, textResult } from "../src/results.js";

test("textResult wraps JSON values as MCP text content", () => {
  const result = textResult({ status: "ok" });
  assert.equal(result.content[0].type, "text");
  assert.match(result.content[0].text, /"status": "ok"/);
});

test("errorResult returns an MCP tool error envelope", () => {
  const result = errorResult(new Error("gateway unavailable"));
  assert.equal(result.isError, true);
  assert.deepEqual(JSON.parse(result.content[0].text), {
    status: "error",
    error: "gateway unavailable",
  });
});

test("errorResult preserves a stable gateway error code", () => {
  const error = Object.assign(new Error("Permanent delete is unsupported."), { code: "permanent_delete_unsupported" });
  const result = errorResult(error);
  assert.deepEqual(JSON.parse(result.content[0].text), {
    status: "error",
    code: "permanent_delete_unsupported",
    error: "Permanent delete is unsupported.",
  });
});

test("jsonToolResult converts thrown errors to error envelopes", async () => {
  const result = await jsonToolResult(async () => {
    throw new Error("invalid or revoked API token");
  });
  assert.equal(result.isError, true);
  assert.match(result.content[0].text, /invalid or revoked API token/);
});
