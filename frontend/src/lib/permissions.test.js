import assert from "node:assert/strict";
import test from "node:test";

import { effectiveRule, maskedToken, permissionLifetimeLabel, permissionsToMap, ruleLabel } from "./permissions.js";

test("permissionsToMap indexes execution rules by server id", () => {
  assert.deepEqual(
    permissionsToMap([
      { server_id: 4, execution_rule: "always_run", expires_at: "2026-06-07T10:00:00Z" },
      { server_id: 7, execution_rule: "approval_required" },
    ]),
    {
      4: { execution_rule: "always_run", expires_at: "2026-06-07T10:00:00Z" },
      7: { execution_rule: "approval_required", expires_at: "" },
    }
  );
});

test("permission helpers treat expired grants as ineffective", () => {
  const now = new Date("2026-06-07T11:00:00Z").getTime();
  assert.equal(effectiveRule({ execution_rule: "always_run", expires_at: "2026-06-07T10:00:00Z" }, now), "");
  assert.equal(effectiveRule({ execution_rule: "always_run", expires_at: "2026-06-07T12:00:00Z" }, now), "always_run");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-07T12:00:00Z" }, now), "1h left");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-07T12:00:01Z" }, now), "1h left");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-07T15:00:01Z" }, now), "4h left");
  assert.equal(permissionLifetimeLabel({ execution_rule: "always_run", expires_at: "2026-06-08T11:00:01Z" }, now), "1d left");
});

test("token and rule helpers produce compact labels", () => {
  assert.equal(maskedToken("aip_1234567890abcdef"), "aip_1234...abcdef");
  assert.equal(maskedToken(""), "legacy token unavailable");
  assert.equal(ruleLabel("approval_required"), "prompt");
});
