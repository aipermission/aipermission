import assert from "node:assert/strict";
import test from "node:test";
import { shellSource, vaultFeatureSource, vaultActionApprovalDialogSource } from "./app-smoke-fixtures.js";

test("Vault keeps values behind explicit local actions", () => {
  assert.match(vaultFeatureSource, /\/api\/vault-items/);
  assert.match(vaultFeatureSource, /\/reveal/);
  assert.match(vaultFeatureSource, /navigator\.clipboard\.writeText/);
  assert.match(vaultFeatureSource, /Replace local value/);
  assert.match(vaultFeatureSource, /Save generated value/);
  assert.match(vaultFeatureSource, /\/generate-preview/);
  assert.match(vaultFeatureSource, /Regenerate/);
  assert.match(vaultFeatureSource, /generator_kind:/);
  assert.match(vaultFeatureSource, /DateTimePicker/);
  assert.match(vaultFeatureSource, /visibleTags/);
  assert.match(vaultFeatureSource, /variant="outline" className="h-9 w-9 px-0"/);
  assert.match(vaultFeatureSource, /expected_value_version/);
  assert.match(vaultFeatureSource, /expected_metadata_revision/);
});

test("Vault Prompt actions expose an explicit local approval dialog", () => {
  assert.match(shellSource, /\/api\/vault-action-approvals/);
  assert.match(shellSource, /runVaultActionApproval/);
  assert.match(shellSource, /declineVaultActionApproval/);
  assert.match(vaultActionApprovalDialogSource, /Every process in that shell can read, transform, persist, or transmit them/);
  assert.match(vaultActionApprovalDialogSource, /generated value stays hidden from the AI/);
  assert.match(vaultActionApprovalDialogSource, /Requested metadata/);
});
