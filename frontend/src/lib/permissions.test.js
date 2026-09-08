import assert from "node:assert/strict";
import test from "node:test";

import { effectiveRule, maskedToken, permissionExpired, permissionLifetimeLabel, ruleLabel } from "./permissions.js";

test("permission helpers treat expired grants as ineffective", () => {
  const now = new Date("2026-06-07T11:00:00Z").getTime();
  assert.equal(effectiveRule({ execution_rule: "always_run", expires_at: "2026-06-07T10:00:00Z" }, now), "");
  assert.equal(effectiveRule({ execution_rule: "always_run", expires_at: "2026-06-07T12:00:00Z" }, now), "always_run");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-07T12:00:00Z" }, now), "1h left");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-07T12:00:01Z" }, now), "1h left");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-07T15:00:01Z" }, now), "4h left");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-08T11:00:01Z" }, now), "1d left");
});

test("permission helpers fail closed for malformed non-empty expiry values", () => {
  const malformed = { execution_rule: "always_run", expires_at: "not-a-timestamp" };

  assert.equal(permissionExpired(malformed), true);
  assert.equal(effectiveRule(malformed), "");
  assert.equal(permissionLifetimeLabel(malformed), "Invalid expiry");
});

test("token and rule helpers produce compact labels", () => {
  assert.equal(maskedToken("aip_1234567890abcdef"), "aip_1234...abcdef");
  assert.equal(maskedToken(""), "token value unavailable");
  assert.equal(ruleLabel("approval_required"), "prompt");
});
