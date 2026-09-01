import assert from "node:assert/strict";
import test from "node:test";
import {
  appSource,
  apiSource,
  nginxSource,
  sidebarSource,
  unlockSource,
  consolePageSource,
  connectorsSource,
  tokensSource,
  credentialsSource,
  connectorTokenPermissionPanelSource,
  connectorPermissionDialogSource,
  vaultPermissionDialogSource,
} from "./app-smoke-fixtures.js";

test("App uses the current unlock API endpoints", () => {
  assert.match(appSource, /apiGet\("\/api\/unlock\/status"\)/);
  assert.match(unlockSource, /apiPost\("\/api\/unlock\/setup"/);
  assert.match(unlockSource, /apiPost\("\/api\/unlock"/);
  assert.doesNotMatch(`${appSource}\n${unlockSource}`, /\/api\/unlock\/create|\/api\/unlock\/open/);
});

test("MCP setup defaults to the local Docker frontend origin", () => {
  assert.match(apiSource, /"http:\/\/localhost:3210"/);
  assert.doesNotMatch(apiSource, /mcpApiUrl[\s\S]*"http:\/\/localhost:8080"/);
});

test("nginx CSP keeps browser connections local plus manual update checks", () => {
  assert.match(nginxSource, /connect-src 'self'/);
  assert.match(nginxSource, /https:\/\/api\.github\.com/);
  assert.doesNotMatch(nginxSource, /ws:\/\/localhost:3210/);
  assert.doesNotMatch(nginxSource, /ws:\/\/localhost:\*/);
});

test("nginx keeps route-specific upload limits and JSON error responses aligned", () => {
  assert.match(nginxSource, /client_max_body_size 256m/);
  assert.match(nginxSource, /location = \/api\/file-transfers\/upload[\s\S]*client_max_body_size 528m/);
  assert.match(nginxSource, /location = \/api\/file-transfers\/upload-batch[\s\S]*client_max_body_size 1040m/);
  assert.match(nginxSource, /error_page 413 = @payload_too_large/);
  assert.match(nginxSource, /Uploaded database is too large/);
  assert.match(nginxSource, /Maximum file size is 512 MiB/);
  assert.match(nginxSource, /Maximum batch size is 1 GiB/);
  assert.doesNotMatch(nginxSource, /proxy_intercept_errors\s+on/);
  assert.doesNotMatch(nginxSource, /error_page 502 503 504/);
});

test("Sidebar exposes explicit MCP runtime start and stop controls", () => {
  assert.match(sidebarSource, /Start MCP/);
  assert.match(sidebarSource, /Stop MCP/);
  assert.match(sidebarSource, /onSetMCPRuntimeEnabled/);
});

test("Token permission controls expose temporary grant lifetimes", () => {
  assert.match(connectorTokenPermissionPanelSource, /import \{ effectiveRule, expiresAtFromLifetime/);
  assert.match(connectorTokenPermissionPanelSource, /ProfileLifetimeControls/);
  assert.match(connectorTokenPermissionPanelSource, /Basic/);
  assert.match(connectorTokenPermissionPanelSource, /Grouped/);
  assert.match(connectorTokenPermissionPanelSource, /Advanced/);
  assert.match(connectorTokenPermissionPanelSource, /inferPermissionMode/);
  assert.match(connectorTokenPermissionPanelSource, /tokenProfileModeKey/);
  assert.match(connectorTokenPermissionPanelSource, /All operations/);
  assert.match(connectorTokenPermissionPanelSource, /connectorActionRiskOrder/);
  assert.match(connectorTokenPermissionPanelSource, /connectorActionRiskGroupLabel/);
  assert.match(connectorTokenPermissionPanelSource, /connectorActionRiskDescription/);
  assert.match(connectorTokenPermissionPanelSource, /onSetTemporary\("1h"\)/);
  assert.match(connectorTokenPermissionPanelSource, /onSetTemporary\("4h"\)/);
  assert.match(connectorTokenPermissionPanelSource, /onSetTemporary\("1d"\)/);
  assert.doesNotMatch(appSource + connectorsSource + credentialsSource + consolePageSource, /PermissionDialog/);
});

test("Token page exposes connector action permissions", () => {
  assert.match(connectorsSource, /Add connector/);
  assert.match(connectorsSource, /import \{ supportedConnectorKinds \}/);
  assert.match(connectorPermissionDialogSource, /\/api\/tokens\/\$\{tokenID\}\/connector-permissions/);
  assert.match(connectorPermissionDialogSource, /\/api\/connectors"/);
  assert.match(connectorPermissionDialogSource, /\/api\/connector-targets\/inventory/);
  assert.match(connectorPermissionDialogSource, /profile\.actions/);
  assert.match(connectorPermissionDialogSource, /approval_required/);
  assert.match(connectorPermissionDialogSource, /always_run/);
  assert.match(connectorPermissionDialogSource, /Save connector permissions/);
  assert.doesNotMatch(connectorPermissionDialogSource, /\/project-capabilities|\/project-scopes/);
  assert.match(vaultPermissionDialogSource, /\/project-capabilities/);
  assert.match(vaultPermissionDialogSource, /Vault capabilities/);
  assert.match(vaultPermissionDialogSource, /Save Vault capabilities/);
  assert.match(vaultPermissionDialogSource, /!max-w-\[1120px\]/);
  assert.match(tokensSource, /VaultPermissionDialog/);
  assert.match(tokensSource, /setVaultPermissionDialog\(token\)/);
});
