import assert from "node:assert/strict";
import test from "node:test";

import { errorResult, jsonToolResult, textResult } from "../src/results.js";
import { gatewayAPIError } from "../src/api-error.js";

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

test("gateway outcome_unknown errors preserve only safe retry metadata", () => {
  const error = gatewayAPIError(
    {
      status: "outcome_unknown",
      code: "connector_action_persistence_unknown",
      error: "Final persistence failed.",
      request_id: 73,
      assistant_hint: "Poll request 73 without creating a new attempt.",
      retry_after_seconds: 3,
      provider_secret: "must-not-escape",
    },
    503,
  );
  const result = errorResult(error);
  const payload = JSON.parse(result.content[0].text);
  assert.deepEqual(payload, {
    status: "outcome_unknown",
    code: "connector_action_persistence_unknown",
    request_id: 73,
    assistant_hint: "Poll request 73 without creating a new attempt.",
    retry_after_seconds: 3,
    error: "Final persistence failed.",
  });
  assert.equal(result.content[0].text.includes("provider_secret"), false);
  assert.equal(result.content[0].text.includes("must-not-escape"), false);
});

test("jsonToolResult converts thrown errors to error envelopes", async () => {
  const result = await jsonToolResult(async () => {
    throw new Error("invalid or revoked API token");
  });
  assert.equal(result.isError, true);
  assert.match(result.content[0].text, /invalid or revoked API token/);
});
