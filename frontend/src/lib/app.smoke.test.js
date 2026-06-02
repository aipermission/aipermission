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
  assert.match(releaseSource, /appVersion = "0\.1\.1"/);
});

test("Sidebar exposes explicit MCP runtime start and stop controls", () => {
  assert.match(sidebarSource, /Start MCP/);
  assert.match(sidebarSource, /Stop MCP/);
  assert.match(sidebarSource, /onSetMCPRuntimeEnabled/);
});

test("Approval dialog warns before persisting command context", () => {
  assert.match(approvalDialogSource, /shell command body/);
  assert.match(approvalDialogSource, /may be persisted/);
  assert.match(approvalDialogSource, /redaction is best-effort/i);
});

test("Server host key dialog handles first approval and changed fingerprints", () => {
  assert.match(serversSource, /unknown_ssh_host_key/);
  assert.match(serversSource, /changed_ssh_host_key/);
  assert.match(serversSource, /Replace trusted fingerprint/);
  assert.match(serversSource, /Previously trusted/);
  assert.match(serversSource, /replace: Boolean\(hostKey\.changed\)/);
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
