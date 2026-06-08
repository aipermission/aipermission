import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import test from "node:test";

const currentDir = dirname(fileURLToPath(import.meta.url));
const appSource = readFileSync(join(currentDir, "..", "App.jsx"), "utf8");
const apiSource = readFileSync(join(currentDir, "api.js"), "utf8");
const nginxSource = readFileSync(join(currentDir, "..", "..", "nginx.conf"), "utf8");
const sidebarSource = readFileSync(join(currentDir, "..", "components", "app-sidebar.jsx"), "utf8");
const unlockSource = readFileSync(join(currentDir, "..", "pages", "unlock.jsx"), "utf8");
const releaseSource = readFileSync(join(currentDir, "release.js"), "utf8");
const approvalDialogSource = readFileSync(join(currentDir, "..", "components", "console", "approval-dialog.jsx"), "utf8");
const serversSource = readFileSync(join(currentDir, "..", "pages", "servers.jsx"), "utf8");
const settingsSource = readFileSync(join(currentDir, "..", "pages", "settings.jsx"), "utf8");
const shellSource = readFileSync(join(currentDir, "..", "components", "app-shell.jsx"), "utf8");
const historySource = readFileSync(join(currentDir, "..", "pages", "history.jsx"), "utf8");
const consolePageSource = readFileSync(join(currentDir, "..", "pages", "console.jsx"), "utf8");
const sshKeysSource = readFileSync(join(currentDir, "..", "pages", "ssh-keys.jsx"), "utf8");
const fileTransferDialogSource = readFileSync(join(currentDir, "..", "components", "console", "file-transfer-dialog.jsx"), "utf8");
const fileTransferBrowserSource = readFileSync(join(currentDir, "..", "components", "console", "file-transfer-browser-dialog.jsx"), "utf8");
const fileTransferConfirmSource = readFileSync(join(currentDir, "..", "components", "console", "file-transfer-confirm-dialogs.jsx"), "utf8");
const transferCenterSource = readFileSync(join(currentDir, "..", "components", "transfer-center.jsx"), "utf8");
const tokenPermissionPanelSource = readFileSync(join(currentDir, "..", "components", "console", "token-permission-panel.jsx"), "utf8");
const permissionDialogSource = readFileSync(join(currentDir, "..", "components", "tokens", "permission-dialog.jsx"), "utf8");

test("App keeps the primary route surface available", () => {
  for (const route of ["/console", "/servers", "/history", "/audit-logs", "/tokens", "/ssh-keys", "/mcp-setup", "/security", "/settings"]) {
    assert.match(appSource, new RegExp(`path="${route}"`));
    assert.match(sidebarSource, new RegExp(`to: "${route}"`));
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
  assert.match(appSource, /useTheme\(\)/);
  assert.match(appSource, /<Shell theme=\{theme\} setTheme=\{setTheme\}/);
  assert.match(sidebarSource, /onSetTheme\("dark"\)/);
  assert.match(sidebarSource, /onSetTheme\("light"\)/);
  assert.match(sidebarSource, /Changelog/);
  assert.match(sidebarSource, /max-h-\[calc\(100vh-180px\)\] overflow-y-auto/);
  assert.match(shellSource, /data\?\.state === "unlocked"/);
  assert.match(shellSource, /document\.title = `\$\{runtimeLabel\} - \$\{databaseName\}`/);
  assert.match(releaseSource, /appVersion = "0\.1\.11"/);
  assert.match(releaseSource, /SSH compatibility polish/);
  assert.match(releaseSource, /Windows checkouts preserve LF line endings/);
});

test("Sidebar exposes explicit MCP runtime start and stop controls", () => {
  assert.match(sidebarSource, /Start MCP/);
  assert.match(sidebarSource, /Stop MCP/);
  assert.match(sidebarSource, /onSetMCPRuntimeEnabled/);
});

test("Token permission controls expose temporary grant lifetimes", () => {
  assert.match(tokenPermissionPanelSource, /PermissionLifetimeControls/);
  assert.match(tokenPermissionPanelSource, /onSetTemporary\("1h"\)/);
  assert.match(tokenPermissionPanelSource, /onSetTemporary\("4h"\)/);
  assert.match(tokenPermissionPanelSource, /onSetTemporary\("1d"\)/);
  assert.match(permissionDialogSource, /Lifetime/);
  assert.match(permissionDialogSource, /1 hour/);
  assert.match(permissionDialogSource, /4 hours/);
  assert.match(permissionDialogSource, /1 day/);
});

test("Approval dialog warns before persisting command context", () => {
  assert.match(approvalDialogSource, /shell command body/);
  assert.match(approvalDialogSource, /may be persisted/);
  assert.match(approvalDialogSource, /redaction is best-effort/i);
  assert.match(approvalDialogSource, /sent \{requestAge\}/);
  assert.match(approvalDialogSource, /action\.state === "stale"/);
  assert.match(approvalDialogSource, /OK/);
  assert.match(consolePageSource, /activeApprovalSnapshot/);
  assert.match(consolePageSource, /isStaleApprovalError/);
});

test("Server host key dialog handles first approval and changed fingerprints", () => {
  assert.match(serversSource, /unknown_ssh_host_key/);
  assert.match(serversSource, /changed_ssh_host_key/);
  assert.match(serversSource, /Replace trusted fingerprint/);
  assert.match(serversSource, /Previously trusted/);
  assert.match(serversSource, /replace: Boolean\(hostKey\.changed\)/);
});

test("Servers page exposes on-demand Docker checks", () => {
  assert.match(serversSource, /\/api\/servers\/\$\{server\.id\}\/docker-check/);
  assert.match(serversSource, /\/api\/servers\/\$\{server\.id\}\/docker-logs/);
  assert.match(serversSource, /Check Docker/);
  assert.match(serversSource, /Container details/);
  assert.match(serversSource, /Container logs/);
  assert.match(serversSource, /No running Docker containers/);
});

test("Servers page exposes advanced SSH startup settings", () => {
  assert.match(serversSource, /Advanced SSH startup/);
  assert.match(serversSource, /startup_input_after_connect/);
  assert.match(serversSource, /force_shell_command/);
  assert.match(serversSource, /Startup input after connect/);
  assert.match(serversSource, /Force shell command/);
  assert.match(serversSource, /QNAP/);
});

test("Servers page can prefill servers from host config", () => {
  assert.match(serversSource, /\/api\/ssh-config\/discover/);
  assert.match(serversSource, /\/api\/ssh-config\/parse/);
  assert.match(serversSource, /Import SSH hosts/);
  assert.match(serversSource, /imports host metadata only/i);
  assert.match(serversSource, /Import from this computer/);
  assert.match(serversSource, /Container config/);
  assert.match(serversSource, /Choose host config file/);
  assert.match(serversSource, /Scan container config/);
  assert.match(serversSource, /Parse pasted hosts/);
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
  assert.match(historySource, /label_id/);
  assert.match(historySource, /source/);
  assert.match(historySource, /SourceBadge/);
  assert.match(historySource, /Not tracked/);
  assert.match(historySource, /Stale/);
  assert.match(historySource, /\/api\/approvals\/\$\{id\}\/labels/);
  assert.doesNotMatch(historySource, /setLabelDialogOpen/);
});

test("Console and History expose SSH file transfer flows", () => {
  assert.match(historySource, /File Transfer History/);
  assert.match(historySource, /\/api\/file-transfers\?/);
  assert.match(historySource, /\/api\/file-transfers\/\$\{item\.id\}\/download/);
  assert.match(historySource, /DirectionBadge/);
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
  assert.match(shellSource, /restartConsoleSession/);
  assert.match(shellSource, /\/api\/console\/servers\/\$\{serverID\}\/restart/);
  assert.match(consolePageSource, /ConsoleRecoveryPanel/);
  assert.match(consolePageSource, /AI command running/);
  assert.match(consolePageSource, /Manual command running/);
  assert.match(consolePageSource, /Looks stuck\? Restart opens a fresh SSH session/);
  assert.match(consolePageSource, /commandPreview/);
  assert.match(consolePageSource, /Restart/);
});

test("Settings page exposes history label management", () => {
  assert.match(settingsSource, /\/api\/history-labels/);
  assert.match(settingsSource, /History labels/);
  assert.match(settingsSource, /Delete history label/);
  assert.match(settingsSource, /removes the label from every related history entry/i);
});

test("SSH keys page supports explicit private key import", () => {
  assert.match(sshKeysSource, /\/api\/ssh-keys\/import/);
  assert.match(sshKeysSource, /Import key/);
  assert.match(sshKeysSource, /Choose key file/);
  assert.match(sshKeysSource, /type="file" onChange=\{readImportFile\}/);
  assert.match(sshKeysSource, /privateKeyPlaceholder/);
  assert.match(sshKeysSource, /The passphrase is not saved/);
});
