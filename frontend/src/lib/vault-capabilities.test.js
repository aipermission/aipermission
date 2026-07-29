import assert from "node:assert/strict";
import test from "node:test";
import {
  vaultCapabilitiesFromDraft,
  vaultCapabilityDraftFromItems,
  vaultCapabilityKey,
} from "./vault-capabilities.js";

test("Vault capability draft preserves temporary grant expiry", () => {
  const definitions = [{ name: "vault.session_apply", allowed_rules: ["approval_required", "always_run"] }];
  const items = [{
    project_id: 7,
    capability_name: "vault.session_apply",
    execution_rule: "always_run",
    expires_at: "2026-08-01T12:00:00Z",
  }];

  const draft = vaultCapabilityDraftFromItems(items, definitions);
  assert.deepEqual(draft[vaultCapabilityKey(7, "vault.session_apply")], {
    execution_rule: "always_run",
    expires_at: "2026-08-01T12:00:00Z",
  });
  assert.deepEqual(
    vaultCapabilitiesFromDraft([{ project_id: 7 }], definitions, draft),
    items,
  );
});
