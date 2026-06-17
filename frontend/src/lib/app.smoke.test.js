import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import test from "node:test";

const currentDir = dirname(fileURLToPath(import.meta.url));
const connectorTemplatesDir = join(currentDir, "..", "connectors", "templates");
const indexSource = readFileSync(join(currentDir, "..", "..", "index.html"), "utf8");
const themeInitSource = readFileSync(join(currentDir, "..", "..", "public", "theme-init.js"), "utf8");
const appSource = readFileSync(join(currentDir, "..", "App.jsx"), "utf8");
const apiSource = readFileSync(join(currentDir, "api.js"), "utf8");
const nginxSource = readFileSync(join(currentDir, "..", "..", "nginx.conf"), "utf8");
const sidebarSource = readFileSync(join(currentDir, "..", "components", "app-sidebar.jsx"), "utf8");
const unlockSource = readFileSync(join(currentDir, "..", "pages", "unlock.jsx"), "utf8");
const releaseSource = readFileSync(join(currentDir, "release.js"), "utf8");
const connectorApprovalDialogSource = readFileSync(join(currentDir, "..", "components", "console", "connector-action-approval-dialog.jsx"), "utf8");
const connectorActivityDialogSource = readFileSync(join(currentDir, "..", "components", "console", "connector-activity-dialog.jsx"), "utf8");
const settingsSource = readFileSync(join(currentDir, "..", "pages", "settings.jsx"), "utf8");
const shellSource = readFileSync(join(currentDir, "..", "components", "app-shell.jsx"), "utf8");
const historySource = readFileSync(join(currentDir, "..", "pages", "history.jsx"), "utf8");
const auditLogsSource = readFileSync(join(currentDir, "..", "pages", "audit-logs.jsx"), "utf8");
const consolePageSource = readFileSync(join(currentDir, "..", "pages", "console.jsx"), "utf8");
const connectorsSource = readFileSync(join(currentDir, "..", "pages", "connectors.jsx"), "utf8");
const credentialsSource = readFileSync(join(currentDir, "..", "pages", "credentials.jsx"), "utf8");
const fileTransferDialogSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "file-transfer-dialog.jsx"), "utf8");
const fileTransferBrowserSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "file-transfer-browser-dialog.jsx"), "utf8");
const fileTransferConfirmSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "file-transfer-confirm-dialogs.jsx"), "utf8");
const bulkCommandDialogSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "bulk-command-dialog.jsx"), "utf8");
const transferCenterSource = readFileSync(join(currentDir, "..", "components", "transfer-center.jsx"), "utf8");
const tokenPermissionPanelSource = readFileSync(join(currentDir, "..", "components", "console", "token-permission-panel.jsx"), "utf8");
const connectorTokenPermissionPanelSource = readFileSync(join(currentDir, "..", "components", "console", "connector-token-permission-panel.jsx"), "utf8");
const connectorPermissionDialogSource = readFileSync(join(currentDir, "..", "components", "tokens", "connector-permission-dialog.jsx"), "utf8");
const connectorTemplateCommonSource = readFileSync(join(currentDir, "..", "connectors", "templates", "common.jsx"), "utf8");
const connectorTargetProfileSaveSource = readFileSync(join(currentDir, "..", "connectors", "templates", "target-profile-save.js"), "utf8");
const connectorTemplateRegistrySource = readFileSync(join(currentDir, "..", "connectors", "templates", "registry.jsx"), "utf8");
const connectorTemplateCatalogSource = readFileSync(join(currentDir, "..", "connectors", "templates", "catalog.js"), "utf8");
const backendConnectorRegistrySource = readFileSync(join(currentDir, "..", "..", "..", "backend", "internal", "connectors", "builtin", "registry.go"), "utf8");
const connectorTemplateKinds = readdirSync(connectorTemplatesDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory())
  .map((entry) => entry.name)
  .sort();
const sshConnectorFormTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "form.jsx"), "utf8");
const sshCredentialFormTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "credential-form.jsx"), "utf8");
const sshCredentialRowActionsTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "credential-row-actions.jsx"), "utf8");
const sshConnectorListItemTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "list-item.jsx"), "utf8");
const sshConnectorConsoleTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "console.jsx"), "utf8");
const sshConnectorIndexSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "index.jsx"), "utf8");
const sshConnectorMetadataSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "metadata.json"), "utf8");
const sshConnectorModelSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "model.js"), "utf8");
const sshConnectorOperationsSource = readFileSync(join(currentDir, "..", "connectors", "templates", "ssh", "operations.jsx"), "utf8");
const postgresConnectorFormTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "form.jsx"), "utf8");
const postgresCredentialFormTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "credential-form.jsx"), "utf8");
const postgresConnectorListItemTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "list-item.jsx"), "utf8");
const postgresConnectorConsoleTemplateSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "console.jsx"), "utf8");
const postgresConnectorIndexSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "index.jsx"), "utf8");
const postgresConnectorMetadataSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "metadata.json"), "utf8");
const postgresConnectorModelSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "model.js"), "utf8");
const postgresConnectorOperationsSource = readFileSync(join(currentDir, "..", "connectors", "templates", "postgres", "operations.jsx"), "utf8");

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function backendRegisteredConnectorKinds(source) {
  const connectorImports = new Map();
  for (const match of source.matchAll(/(\w+)\s+"github\.com\/aipermission\/aipermission\/backend\/internal\/connectors\/([^"]+)"/g)) {
    const alias = match[1];
    const parts = match[2].split("/");
    connectorImports.set(alias, parts[0]);
  }
  const kinds = [];
  for (const match of source.matchAll(/(\w+)\.New\(\)/g)) {
    const kind = connectorImports.get(match[1]);
    if (kind) kinds.push(kind);
  }
  return [...new Set(kinds)].sort();
}

test("App keeps the primary route surface available", () => {
  for (const route of ["/console", "/connectors", "/history", "/audit-logs", "/tokens", "/credentials", "/mcp-setup", "/security", "/settings"]) {
    assert.match(appSource, new RegExp(`path="${route}"`));
    assert.match(sidebarSource, new RegExp(`to: "${route}"`));
  }
  assert.match(appSource, /path="\/servers"/);
  assert.match(appSource, /<Navigate to="\/connectors" replace/);
  assert.doesNotMatch(sidebarSource, /to: "\/servers"/);
  assert.match(shellSource, /Promise\.allSettled/);
});

test("Connectors page wires generic connector templates", () => {
  assert.match(connectorsSource, /Add connector/);
  assert.match(connectorsSource, /Connector type/);
  assert.match(connectorsSource, /Operations/);
  assert.match(connectorsSource, /showUnderConstruction/);
  assert.match(connectorsSource, /getConnectorTemplate\(form\.connector_kind\)\?\.Form/);
  assert.match(connectorsSource, /getConnectorModel\(target\.connector_kind\)/);
  assert.match(connectorsSource, /model\.save/);
  assert.match(connectorsSource, /model\.deleteTarget/);
  assert.match(connectorsSource, /model\.test/);
  assert.match(connectorTemplateRegistrySource, /import\.meta\.glob\("\.\/\*\/index\.jsx"/);
  assert.match(connectorTemplateCatalogSource, /import\.meta\.glob\("\.\/\*\/metadata\.json"/);
  assert.doesNotMatch(connectorTemplateRegistrySource, /ssh:\s*Object\.freeze|postgres:\s*Object\.freeze/);
  assert.match(connectorTemplateCatalogSource, /supportedConnectorKinds/);
  assert.match(connectorTemplateRegistrySource, /templateModules/);
  assert.match(connectorTemplateRegistrySource, /connectorKindFromPath/);
  assert.match(sshConnectorIndexSource, /CredentialForm/);
  assert.match(sshConnectorIndexSource, /ToolbarActions/);
  assert.match(postgresConnectorIndexSource, /CredentialForm/);
  assert.match(postgresConnectorIndexSource, /Operations/);
  assert.match(postgresConnectorIndexSource, /ToolbarActions/);
  assert.match(sshConnectorMetadataSource, /"label": "SSH"/);
  assert.match(sshConnectorMetadataSource, /"icon": "server"/);
  assert.match(postgresConnectorMetadataSource, /"label": "Postgres"/);
  assert.match(postgresConnectorMetadataSource, /"icon": "database"/);
  assert.match(connectorTemplateRegistrySource, /ConnectorTemplateNotFound/);
  assert.match(sshConnectorFormTemplateSource, /SSHConnectorFormTemplate/);
  assert.match(sshConnectorListItemTemplateSource, /SSHConnectorRowActionsTemplate/);
  assert.match(sshConnectorConsoleTemplateSource, /SSHConnectorConsoleTemplate/);
  assert.match(sshConnectorModelSource, /apiPost\("\/api\/connector-targets\/test"/);
  assert.match(sshConnectorModelSource, /createTargetWithProfile/);
  assert.match(sshConnectorModelSource, /updateTargetWithProfile/);
  assert.match(sshConnectorModelSource, /apiDelete\(`\/api\/connector-targets\/\$\{target\.id\}/);
  assert.doesNotMatch(sshConnectorModelSource, /\/api\/servers\/test-connection/);
  assert.match(sshConnectorModelSource, /\/profiles\/\$\{selectedProfile\.id\}\/test/);
  assert.doesNotMatch(sshConnectorModelSource, /apiPut\(`\/api\/servers\//);
  assert.doesNotMatch(sshConnectorModelSource, /apiDelete\(`\/api\/servers\//);
  assert.match(sshConnectorModelSource, /deleteDialog/);
  assert.match(postgresConnectorFormTemplateSource, /PostgresConnectorFormTemplate/);
  assert.match(postgresConnectorListItemTemplateSource, /PostgresConnectorRowActionsTemplate/);
  assert.match(postgresConnectorConsoleTemplateSource, /PostgresConnectorConsoleTemplate/);
  assert.match(postgresConnectorModelSource, /createTargetWithProfile/);
  assert.match(postgresConnectorModelSource, /updateTargetWithProfile/);
  assert.match(postgresConnectorModelSource, /apiDelete\(`\/api\/connector-targets\/\$\{target\.id\}`/);
  assert.match(postgresConnectorModelSource, /\/profiles\/\$\{selectedProfile\.id\}\/test/);
  assert.match(postgresConnectorModelSource, /deleteDialog/);
  assert.match(connectorTargetProfileSaveSource, /apiPost\("\/api\/connector-targets\/with-profile"/);
  assert.match(connectorTargetProfileSaveSource, /apiPut\(`\/api\/connector-targets\/\$\{targetID\}\/with-profile\/\$\{profileID\}`/);
  assert.doesNotMatch(connectorTargetProfileSaveSource, /apiDelete/);
  assert.match(sshConnectorModelSource, /function targetEndpoint/);
  assert.match(postgresConnectorModelSource, /function targetEndpoint/);
  assert.match(connectorsSource, /model\?\.targetEndpoint/);
  assert.doesNotMatch(connectorTemplateCommonSource, /target\.connector_kind ===/);
  assert.doesNotMatch(connectorTemplateCommonSource, /kind === "ssh"|kind === "postgres"/);
  assert.match(sshConnectorListItemTemplateSource, /Check Docker/);
  assert.match(sshConnectorListItemTemplateSource, /Install key/);
  assert.match(sshConnectorListItemTemplateSource, /Install key for/);
  assert.match(postgresConnectorListItemTemplateSource, /Create managed DB user/);
  assert.match(postgresConnectorListItemTemplateSource, /Backup \/ restore database/);
  assert.doesNotMatch(postgresConnectorListItemTemplateSource, /Export table JSON/);
  assert.match(postgresConnectorOperationsSource, /\/profiles\/\$\{profileID\}\/provision/);
  assert.match(postgresConnectorOperationsSource, /\/backup/);
  assert.match(postgresConnectorOperationsSource, /\/restore/);
  assert.doesNotMatch(postgresConnectorOperationsSource, /export_table_json/);
  assert.match(postgresConnectorOperationsSource, /Create user/);
  assert.doesNotMatch(sshConnectorListItemTemplateSource, /Delete SSH connector|Edit SSH connector|Test SSH/);
  assert.match(connectorsSource, /title="Test connection"/);
  assert.match(connectorsSource, /title="Edit connector"/);
  assert.match(connectorsSource, /title="Delete connector"/);
  assert.match(connectorsSource, /\/api\/connectors\/\$\{item\.kind\}/);
  assert.match(connectorsSource, /\/api\/connector-targets\/inventory/);
  assert.match(credentialsSource, /\/api\/connector-targets\/inventory/);
  assert.doesNotMatch(`${connectorsSource}\n${credentialsSource}`, /apiGet\(`\/api\/connector-targets\/\$\{target\.id\}`/);
  assert.doesNotMatch(connectorsSource, /connection_mode:\s*"direct"/);
  assert.doesNotMatch(connectorsSource, /apiPut\(`\/api\/connector-targets\/\$\{target\.id\}`/);
  assert.doesNotMatch(connectorsSource, /apiPut\(`\/api\/servers\/\$\{runtimeID\}`/);
  assert.doesNotMatch(connectorsSource, /\/api\/servers\//);
  assert.doesNotMatch(connectorsSource, /\/api\/ssh-host-keys/);
  assert.doesNotMatch(connectorsSource, /\/profiles\/\$\{profile\.id\}\/test/);
  assert.doesNotMatch(shellSource, /apiGet\("\/api\/servers"\)/);
  assert.doesNotMatch(shellSource, /\/api\/connectors\/ssh\/credentials/);
  assert.doesNotMatch(shellSource, /ssh_key_id|target\.config\?\.host|target\.config\?\.username/);
  assert.match(shellSource, /loadCredentialResources/);
  assert.match(sshConnectorModelSource, /loadCredentialResources/);
  assert.match(sshConnectorModelSource, /liveConsoleRuntimeTarget/);
  assert.match(shellSource, /liveConsoleRuntimeTargets/);
});

test("Connector template folders are registered in catalog and registry", () => {
  assert.ok(connectorTemplateKinds.includes("postgres"));
  assert.ok(connectorTemplateKinds.includes("ssh"));
  assert.deepEqual(backendRegisteredConnectorKinds(backendConnectorRegistrySource), connectorTemplateKinds);
  for (const kind of connectorTemplateKinds) {
    const indexSource = readFileSync(join(connectorTemplatesDir, kind, "index.jsx"), "utf8");
    const metadataSource = readFileSync(join(connectorTemplatesDir, kind, "metadata.json"), "utf8");
    assert.match(indexSource, /export default Object\.freeze/);
    assert.match(metadataSource, /"version"/);
  }
});

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

test("nginx accepts encrypted database imports without HTML error pages", () => {
  assert.match(nginxSource, /client_max_body_size 256m/);
  assert.match(nginxSource, /error_page 413 = @payload_too_large/);
  assert.match(nginxSource, /Uploaded database is too large/);
  assert.doesNotMatch(nginxSource, /proxy_intercept_errors\s+on/);
  assert.doesNotMatch(nginxSource, /error_page 502 503 504/);
});

test("App applies the persisted theme before unlock and exposes bundled changelog metadata", () => {
  assert.match(indexSource, /<script src="\/theme-init\.js"><\/script>/);
  assert.doesNotMatch(indexSource, /localStorage\.getItem\("aipermission-theme"\)/);
  assert.match(themeInitSource, /localStorage\.getItem\("aipermission-theme"\)/);
  assert.match(appSource, /useTheme\(\)/);
  assert.match(appSource, /<Shell theme=\{theme\} setTheme=\{setTheme\}/);
  assert.match(sidebarSource, /onSetTheme\("dark"\)/);
  assert.match(sidebarSource, /onSetTheme\("light"\)/);
  assert.match(sidebarSource, /Changelog/);
  assert.match(sidebarSource, /max-h-\[calc\(100vh-180px\)\] overflow-y-auto/);
  assert.match(shellSource, /data\?\.state === "unlocked"/);
  assert.match(shellSource, /document\.title = `\$\{runtimeLabel\} - \$\{databaseName\}`/);
  assert.match(releaseSource, /appVersion = "0\.2\.2"/);
  assert.match(releaseSource, /Postgres management/);
  assert.match(releaseSource, /Maintenance hardening/);
  assert.match(releaseSource, /Updated golang\.org\/x\/crypto to 0\.53\.0/);
  assert.match(releaseSource, /Connector-native baseline/);
  assert.match(releaseSource, /SSH and Postgres now run through the same connector target/);
  assert.match(releaseSource, /Pre-0\.2 preview databases are not opened directly by the normal gateway/);
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
});

test("Console exposes connector action approvals", () => {
  assert.match(shellSource, /\/api\/targets/);
  assert.match(shellSource, /\/api\/connector-action-approvals/);
  assert.doesNotMatch(shellSource, /\/api\/approvals/);
  assert.doesNotMatch(consolePageSource, /components\/console\/approval-dialog|<ApprovalDialog\b|activeApprovalSnapshot/);
  assert.equal(consolePageSource.includes("run" + "Approval"), false);
  assert.equal(consolePageSource.includes("decline" + "Approval"), false);
  assert.doesNotMatch(consolePageSource, /\/api\/approvals/);
  assert.doesNotMatch(bulkCommandDialogSource, /\/api\/approvals/);
  assert.match(bulkCommandDialogSource, /\/api\/console\/command-requests\/\$\{item\.request_id\}/);
  assert.match(consolePageSource, /ConnectorActionApprovalDialog/);
  assert.match(consolePageSource, /ConnectorActivityDialog/);
  assert.match(consolePageSource, /SelectedConnectorConsoleTemplate/);
  assert.match(consolePageSource, /selectedConnectorTemplate\?\.Console/);
  assert.match(consolePageSource, /useConnectorPermissions/);
  assert.match(postgresConnectorConsoleTemplateSource, /PostgresConnectorToolbarActionsTemplate/);
  assert.match(postgresConnectorConsoleTemplateSource, /Schema browser/);
  assert.match(postgresConnectorConsoleTemplateSource, /Search schemas or tables/);
  assert.match(postgresConnectorConsoleTemplateSource, /prepareTableQuery/);
  assert.match(postgresConnectorConsoleTemplateSource, /filteredTableBrowserRows/);
  assert.match(postgresConnectorConsoleTemplateSource, /rowsToCSVText/);
  assert.match(postgresConnectorConsoleTemplateSource, /postgres-result\.csv/);
  assert.match(postgresConnectorConsoleTemplateSource, /postgres-result\.json/);
  assert.match(postgresConnectorConsoleTemplateSource, /Session requests/);
  assert.match(postgresConnectorConsoleTemplateSource, /No active Postgres session/);
  assert.match(postgresConnectorConsoleTemplateSource, /monaco-editor\/esm\/vs\/editor\/editor\.api/);
  assert.match(postgresConnectorConsoleTemplateSource, /action_name: "query_readonly"/);
  assert.match(postgresConnectorConsoleTemplateSource, /action_name: "describe_table"/);
  assert.match(postgresConnectorConsoleTemplateSource, /FROM pg_class c/);
  assert.match(postgresConnectorConsoleTemplateSource, /json_agg/);
  assert.match(postgresConnectorConsoleTemplateSource, /a\.attnum/);
  assert.match(postgresConnectorConsoleTemplateSource, /ChevronRight/);
  assert.match(postgresConnectorConsoleTemplateSource, /referencedTablesFromSQL/);
  assert.match(postgresConnectorConsoleTemplateSource, /tableMatchesReference/);
  assert.match(postgresConnectorConsoleTemplateSource, /CompletionItemKind\.Field/);
  assert.match(postgresConnectorConsoleTemplateSource, /fixedOverflowWidgets: true/);
  assert.match(postgresConnectorConsoleTemplateSource, /suggestController\.js/);
  assert.match(postgresConnectorConsoleTemplateSource, /acceptSuggestionOnEnter: "on"/);
  assert.match(postgresConnectorConsoleTemplateSource, /KeyMod\.CtrlCmd \| monacoInstance\.KeyCode\.Enter/);
  assert.match(postgresConnectorConsoleTemplateSource, /Run SQL \(Ctrl\+Enter\)/);
  assert.match(postgresConnectorConsoleTemplateSource, /Result View/);
  assert.match(consolePageSource, /structuredSessionsByTarget/);
  assert.match(consolePageSource, /onNewStructuredSession/);
  assert.match(consolePageSource, /onNewStructuredSession=\{startStructuredConnectorSession\}/);
  assert.match(consolePageSource, /target=/);
  assert.match(consolePageSource, /Search connectors/);
  assert.match(consolePageSource, /Connectors/);
  assert.match(consolePageSource, /targetUsesLiveConsole/);
  assert.match(consolePageSource, /getConnectorModel/);
  assert.match(consolePageSource, /ConnectorIcon/);
  assert.match(tokenPermissionPanelSource, /ConnectorTokenPermissionPanel/);
  assert.match(connectorTokenPermissionPanelSource, /connectorPermissionState/);
  assert.match(connectorTokenPermissionPanelSource, /loadConnectorActions\?\.\(\{ \.\.\.selectedTarget, profile_id: profile\.profile_id/);
  assert.match(connectorTokenPermissionPanelSource, /connectorActionCacheKey\(selectedTarget, profile\.profile_id\)/);
  assert.match(connectorPermissionDialogSource, /\/api\/connector-targets\/inventory/);
  assert.match(connectorTokenPermissionPanelSource, /ProfileLifetimeControls/);
  assert.match(sshConnectorConsoleTemplateSource, /SSHConnectorToolbarActionsTemplate/);
  assert.match(consolePageSource, /pendingConnectorApprovals/);
  assert.match(consolePageSource, /runConnectorActionApproval/);
  assert.match(consolePageSource, /declineConnectorActionApproval/);
  assert.match(connectorApprovalDialogSource, /structured connector action/);
  assert.match(connectorApprovalDialogSource, /Decline note/);
  assert.match(connectorActivityDialogSource, /Recent structured connector requests/);
  assert.match(connectorActivityDialogSource, /always-run requests/);
});

test("SSH connector template owns SSH-specific operations", () => {
  assert.match(sshConnectorModelSource, /unknown_ssh_host_key/);
  assert.match(sshConnectorModelSource, /changed_ssh_host_key/);
  assert.match(sshConnectorOperationsSource, /Replace trusted fingerprint/);
  assert.match(sshConnectorOperationsSource, /Previously trusted/);
  assert.match(sshConnectorOperationsSource, /replace: Boolean\(hostKey\.changed\)/);
  assert.match(sshConnectorOperationsSource, /\/api\/ssh-host-keys\/approve/);
  assert.match(sshConnectorOperationsSource, /Check Docker/);
  assert.match(sshConnectorOperationsSource, /Container details/);
  assert.match(sshConnectorOperationsSource, /Container logs/);
  assert.match(sshConnectorOperationsSource, /No running Docker containers/);
  assert.match(sshConnectorModelSource, /\/api\/connector-targets\/\$\{target\.id\}\/operations\/docker-check/);
  assert.match(sshConnectorModelSource, /\/api\/connector-targets\/\$\{target\.id\}\/operations\/docker-logs/);
  assert.doesNotMatch(sshConnectorModelSource, /\/api\/servers\/\$\{server\.id\}\/docker/);
  assert.match(sshConnectorFormTemplateSource, /Advanced SSH startup/);
  assert.match(sshConnectorFormTemplateSource, /startup_input_after_connect/);
  assert.match(sshConnectorFormTemplateSource, /force_shell_command/);
  assert.match(sshConnectorFormTemplateSource, /Startup input after connect/);
  assert.match(sshConnectorFormTemplateSource, /Force shell command/);
  assert.match(sshConnectorFormTemplateSource, /QNAP/);
});

test("Settings database delete requires a confirmation dialog and current password", () => {
  assert.match(settingsSource, /onSubmit=\{requestDeleteDatabase\}/);
  assert.match(settingsSource, /setDeleteDialogOpen\(true\)/);
  assert.match(settingsSource, /autoFocusClose=\{false\}/);
  assert.match(settingsSource, /Current database password/);
  assert.match(settingsSource, /deletePasswordRef/);
  assert.match(settingsSource, /current_password: deletePassword/);
  assert.doesNotMatch(settingsSource, /onSubmit=\{deleteDatabase\}[\s\S]*Delete<\/CardTitle>/);
});

test("History page exposes label filtering and item label endpoints", () => {
  assert.match(historySource, /\/api\/history-labels/);
  assert.match(historySource, /\/api\/history\?/);
  assert.match(historySource, /\/api\/history\/\$\{item\.id\}/);
  assert.match(historySource, /\/api\/history\/\$\{id\}\/labels/);
  assert.match(historySource, /All connectors/);
  assert.match(historySource, /targetRef/);
  assert.match(historySource, /target_id/);
  assert.match(historySource, /runtime_id/);
  assert.match(historySource, /label_id/);
  assert.match(historySource, /source/);
  assert.match(historySource, /connector_kind/);
  assert.doesNotMatch(historySource, /All activity/);
  assert.match(historySource, /SourceBadge/);
  assert.match(historySource, /Not tracked/);
  assert.match(historySource, /Stale/);
  assert.doesNotMatch(historySource, /setLabelDialogOpen/);
});

test("Audit page exposes connector-aware filters", () => {
  assert.match(auditLogsSource, /connector_kind/);
  assert.match(auditLogsSource, /target_id/);
  assert.match(auditLogsSource, /All connectors/);
  assert.match(auditLogsSource, /auditTargetLabel/);
  assert.match(auditLogsSource, /connectorKindOptions/);
});

test("Console and History expose SSH file transfer flows", () => {
  assert.match(historySource, /file_transfer/);
  assert.match(historySource, /TransferDetail/);
  assert.match(historySource, /\/api\/file-transfers\/\$\{item\.source_ref_id\}\/download/);
  assert.match(historySource, /Save download/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/upload-batch/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/download-batch/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfers\/browse/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/pause/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/resume/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/cancel/);
  assert.match(fileTransferDialogSource, /\/api\/file-transfer-batches\/\$\{batch\.item\.id\}\/queue/);
  assert.match(fileTransferDialogSource, /remote_files_exist/);
  assert.match(fileTransferConfirmSource, /Overwrite all/);
  assert.match(`${fileTransferDialogSource}\n${fileTransferBrowserSource}\n${fileTransferConfirmSource}`, /closeOnOverlay=\{false\}/);
  assert.match(fileTransferDialogSource, /apiPostForm/);
  assert.match(fileTransferDialogSource, /short-lived local staging files/);
  assert.match(shellSource, /\/api\/file-transfer-batches\?limit=30/);
  assert.match(sidebarSource, /Transfers/);
  assert.match(transferCenterSource, /Transfer Center/);
  assert.match(transferCenterSource, /Closing this panel does not stop transfers/);
  assert.match(transferCenterSource, /pending_approval/);
  assert.match(transferCenterSource, /Approve selected/);
  assert.match(shellSource, /\/api\/file-transfer-batches\/\$\{batchID\}\/approve/);
  assert.match(shellSource, /\/api\/file-transfer-batches\/\$\{batchID\}\/decline/);
});

test("Console exposes stuck command recovery controls", () => {
  assert.match(shellSource, /restartConsoleRuntime/);
  assert.match(shellSource, /\/api\/console\/runtime-surfaces\/\$\{runtimeID\}\/restart/);
  assert.match(consolePageSource, /ConsoleRecoveryPanel/);
  assert.match(consolePageSource, /AI command running/);
  assert.match(consolePageSource, /Manual command running/);
  assert.match(consolePageSource, /Looks stuck\? Restart opens a fresh console session/);
  assert.match(consolePageSource, /commandPreview/);
  assert.match(consolePageSource, /Restart/);
});

test("Console exposes bulk command execution controls", () => {
  assert.doesNotMatch(consolePageSource, /BulkCommandDialog/);
  assert.match(sshConnectorConsoleTemplateSource, /BulkCommandDialog/);
  assert.match(sshConnectorConsoleTemplateSource, /Bulk/);
  assert.match(bulkCommandDialogSource, /\/api\/console\/bulk-exec/);
  assert.match(bulkCommandDialogSource, /RUN ON \$\{selectedIDs\.length\} TARGETS/);
  assert.match(bulkCommandDialogSource, /Run selected/);
});

test("Settings page exposes history label management", () => {
  assert.match(settingsSource, /\/api\/history-labels/);
  assert.match(settingsSource, /History labels/);
  assert.match(settingsSource, /Delete history label/);
  assert.match(settingsSource, /removes the label from every related history entry/i);
});

test("Credentials page supports explicit private key import", () => {
  assert.match(credentialsSource, /Add credential/);
  assert.match(credentialsSource, /Operations/);
  assert.match(credentialsSource, /Actions/);
  assert.match(credentialsSource, /getConnectorTemplate\(drawer\.kind\)\?\.CredentialForm/);
  assert.match(credentialsSource, /model\.saveCredential/);
  assert.match(credentialsSource, /model\.deleteCredential/);
  assert.match(sshConnectorModelSource, /\/api\/connectors\/ssh\/credentials\/import/);
  assert.match(sshConnectorModelSource, /apiPut\(`\/api\/connectors\/ssh\/credentials\/\$\{row\.id\}`/);
  assert.match(postgresConnectorModelSource, /\/api\/connector-targets\/\$\{form\.target_id\}\/profiles/);
  assert.match(postgresConnectorModelSource, /apiPut\(`\/api\/connector-targets\/\$\{form\.target_id\}\/profiles\/\$\{row\.id\}`/);
  assert.doesNotMatch(credentialsSource, /apiPost|apiPut|apiDelete/);
  assert.match(credentialsSource, /Edit credential/);
  assert.match(sshCredentialFormTemplateSource, /formMode === "edit"/);
  assert.match(sshCredentialFormTemplateSource, /Save SSH credential/);
  assert.match(sshCredentialFormTemplateSource, /Import credential/);
  assert.match(sshCredentialFormTemplateSource, /Choose key file/);
  assert.match(sshCredentialFormTemplateSource, /type="file" onChange=\{onReadImportFile\}/);
  assert.match(sshCredentialFormTemplateSource, /privateKeyPlaceholder/);
  assert.match(sshCredentialFormTemplateSource, /The passphrase is not saved/);
  assert.match(sshCredentialRowActionsTemplateSource, /install_command/);
  assert.match(credentialsSource, /CredentialRowActionsTemplate \? <CredentialRowActionsTemplate row=\{row\}/);
  assert.doesNotMatch(credentialsSource, /CopyButton/);
  assert.match(postgresCredentialFormTemplateSource, /Create Postgres credential/);
  assert.match(postgresCredentialFormTemplateSource, /Save Postgres credential/);
  assert.match(postgresCredentialFormTemplateSource, /Leave blank to keep current password/);
  assert.match(postgresCredentialFormTemplateSource, /Select Postgres target/);
  assert.match(postgresCredentialFormTemplateSource, /Password/);
});
