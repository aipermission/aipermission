import assert from "node:assert/strict";
import test from "node:test";
import { unlockSource, settingsSource, backupRetentionPanelSource, shellSource } from "./app-smoke-fixtures.js";

test("Settings database delete requires a confirmation dialog and current password", () => {
  assert.match(settingsSource, /onSubmit=\{requestDeleteDatabase\}/);
  assert.match(settingsSource, /setDeleteDialogOpen\(true\)/);
  assert.match(settingsSource, /autoFocusClose=\{false\}/);
  assert.match(settingsSource, /Current database password/);
  assert.match(settingsSource, /deletePasswordRef/);
  assert.match(settingsSource, /current_password: deletePassword/);
  assert.doesNotMatch(settingsSource, /onSubmit=\{deleteDatabase\}[\s\S]*Delete<\/CardTitle>/);
});

test("Settings page exposes a local-only maintenance console", () => {
  assert.match(settingsSource, /Maintenance console/);
  assert.match(settingsSource, /Open maintenance console/);
  assert.match(settingsSource, /\/api\/settings\/maintenance-console\/open/);
  assert.match(settingsSource, /\/api\/settings\/maintenance-console\/attach/);
  assert.match(settingsSource, /\/api\/settings\/maintenance-console\/close/);
  assert.match(settingsSource, /PtyConsole/);
  assert.match(settingsSource, /not exposed to MCP/i);
  assert.match(settingsSource, /Avoid printing secrets/i);
});

test("Settings and unlock expose the self-hosted encrypted backup flow", () => {
  assert.match(settingsSource, /Remote backup providers/);
  assert.match(settingsSource, /Add provider/);
  assert.match(settingsSource, /\/api\/backup\/providers\/catalog/);
  assert.match(settingsSource, /\/api\/backup\/providers/);
  assert.match(settingsSource, /Edit backup provider/);
  assert.match(settingsSource, /Archive backup provider/);
  assert.match(settingsSource, /Enable remote backups/);
  assert.match(settingsSource, /\/enable/);
  assert.match(settingsSource, /\/test/);
  assert.match(settingsSource, /Upload backup/);
  assert.match(settingsSource, /Estimated upload size/);
  assert.match(settingsSource, /Remote backup records/);
  assert.match(settingsSource, /Restore remote backup/);
  assert.match(settingsSource, /\/records\/\$\{record\.id\}\/restore/);
  assert.match(settingsSource, /\/upload/);
  assert.match(settingsSource, /base_url/);
  assert.match(settingsSource, /service token/i);
  assert.match(settingsSource, /docs\/providers\/aipermission-backup\.md/);
  assert.match(settingsSource, /Remote providers store encrypted database files only/);
  assert.match(unlockSource, /Restore from AIPermission Backup/);
  assert.match(unlockSource, /\/api\/backup\/remote\/list/);
  assert.match(unlockSource, /\/api\/backup\/remote\/restore/);
  assert.match(unlockSource, /not\s+saved in browser\s+storage/i);
  assert.match(unlockSource, /<optgroup/);
  assert.match(unlockSource, /formatLocalTimestamp/);
  assert.match(unlockSource, /Source:/);
  assert.match(shellSource, /\/api\/backup\/freshness/);
  assert.match(shellSource, /A newer encrypted backup is available/);
  assert.match(shellSource, /Backup freshness could not be checked/);
});

test("Unlock page shows the current app version", () => {
  assert.match(unlockSource, /appVersion/);
  assert.match(unlockSource, /AIPermission \{appVersion\}/);
});

test("Settings backup records expose retention and quota controls", () => {
  const recordsDialogIndex = settingsSource.indexOf('title="Remote backup records"');
  const retentionPanelIndex = settingsSource.indexOf("<BackupRetentionPanel");
  assert.ok(recordsDialogIndex >= 0 && retentionPanelIndex > recordsDialogIndex);
  assert.match(backupRetentionPanelSource, /\/storage/);
  assert.match(backupRetentionPanelSource, /\/retention\/preview/);
  assert.match(backupRetentionPanelSource, /apply_now/);
});

test("Settings page exposes history label management", () => {
  assert.match(settingsSource, /\/api\/history-labels/);
  assert.match(settingsSource, /History labels/);
  assert.match(settingsSource, /Delete history label/);
  assert.match(settingsSource, /removes the label from every related history entry/i);
});
