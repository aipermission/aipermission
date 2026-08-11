import assert from "node:assert/strict";
import test from "node:test";
import {
  historySource,
  auditLogsSource,
  consolePageSource,
  connectorsSource,
  projectsSource,
  connectorTokenPermissionPanelSource,
  vaultPermissionDialogSource,
  connectorTargetProfileSaveSource,
} from "./app-smoke-fixtures.js";

test("Projects group connector targets and scope token visibility", () => {
  assert.match(projectsSource, /apiGet\("\/api\/projects"\)/);
  assert.match(projectsSource, /apiPost\("\/api\/projects"/);
  assert.match(projectsSource, /apiPut\(`\/api\/projects\/\$\{editor\.project\.id\}`/);
  assert.match(projectsSource, /apiDelete\(`\/api\/projects\/\$\{remove\.project\.id\}`/);
  assert.match(connectorsSource, /project_id/);
  assert.match(connectorsSource, /ProjectTargetRows/);
  assert.match(consolePageSource, /project_name \|\| "Ungrouped"/);
  assert.match(connectorTargetProfileSaveSource, /project_id: Number\(projectID\) \|\| 0/);
  assert.match(vaultPermissionDialogSource, /\/project-scopes/);
  assert.match(vaultPermissionDialogSource, /Changes apply immediately|control whether this token can discover it/);
  assert.match(connectorTokenPermissionPanelSource, /\/project-scopes/);
  assert.match(historySource, /project_id/);
  assert.match(auditLogsSource, /project_id/);
});
