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
  for (const expires_at of [
    "not-a-timestamp",
    "2099",
    "12/31/2099",
    "2099-02-30T00:00:00Z",
    "2099-01-01T24:00:00Z",
    "2099-01-01T00:00:00+24:00",
  ]) {
    const malformed = { execution_rule: "always_run", expires_at };
    assert.equal(permissionExpired(malformed), true, expires_at);
    assert.equal(effectiveRule(malformed), "", expires_at);
    assert.equal(permissionLifetimeLabel(malformed), "Invalid expiry", expires_at);
  }
});

test("permission helpers accept canonical RFC3339 offsets and fractional seconds", () => {
  const now = Date.parse("2026-06-07T11:00:00Z");
  for (const expires_at of ["2026-06-07T12:00:00Z", "2026-06-07T15:00:00.123456789+03:00"]) {
    assert.equal(effectiveRule({ execution_rule: "always_run", expires_at }, now), "always_run", expires_at);
  }
});

test("token and rule helpers produce compact labels", () => {
  assert.equal(maskedToken("aip_1234567890abcdef"), "aip_1234...abcdef");
  assert.equal(maskedToken(""), "token value unavailable");
  assert.equal(ruleLabel("approval_required"), "prompt");
});
