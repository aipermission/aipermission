import assert from "node:assert/strict";
import test from "node:test";
import {
  indexSource,
  themeInitSource,
  appSource,
  sidebarSource,
  releaseSource,
  releaseManifest,
  shellSource,
} from "./smoke/app-smoke-fixtures.js";

test("App keeps the primary route surface available", () => {
  for (const route of [
    "/console",
    "/projects",
    "/vault",
    "/connectors",
    "/history",
    "/audit-logs",
    "/tokens",
    "/credentials",
    "/mcp-setup",
    "/security",
    "/settings",
  ]) {
    assert.match(appSource, new RegExp(`path="${route}"`));
    assert.match(sidebarSource, new RegExp(`to: "${route}"`));
  }
  assert.match(appSource, /path="\/servers"/);
  assert.match(appSource, /<Navigate to="\/connectors" replace/);
  assert.doesNotMatch(sidebarSource, /to: "\/servers"/);
  assert.match(shellSource, /Promise\.allSettled/);
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
  assert.match(releaseSource, new RegExp(`appVersion = "${releaseManifest.version.replaceAll(".", "\\.")}"`));
  assert.match(releaseSource, /Maintenance hardening/);
  assert.match(releaseSource, /Controlled Mail workflows/);
  assert.match(releaseSource, /Backup recovery cleanup/);
  assert.match(releaseSource, /Self-hosted encrypted backups/);
  assert.match(releaseSource, /Guarded Kafka writes/);
  assert.match(releaseSource, /Kafka \/ Redpanda read browser/);
  assert.match(releaseSource, /Redis \/ Valkey compatibility/);
  assert.match(releaseSource, /Security and dependency maintenance/);
  assert.match(releaseSource, /Projects and scoped visibility/);
  assert.match(releaseSource, /MCP target discovery only returns targets from projects enabled for the calling token/);
  assert.match(releaseSource, /S3 MCP polish/);
  assert.match(releaseSource, /S3 list responses now include assistant_hints/);
  assert.match(releaseSource, /S3 connector/);
  assert.match(releaseSource, /S3 is now a built-in connector/);
  assert.match(releaseSource, /Live-console recovery behavior is now defined by connector templates/);
  assert.match(releaseSource, /Kubernetes connector/);
  assert.match(releaseSource, /Kubernetes is now a built-in connector/);
  assert.match(releaseSource, /Docker inventory and images/);
  assert.match(releaseSource, /Docker connector actions now include scoped image/);
  assert.match(releaseSource, /Maintenance and backup providers/);
  assert.match(releaseSource, /Settings now includes a local-only realtime Maintenance Console/);
  assert.match(releaseSource, /Backup providers are storage metadata only/);
  assert.match(releaseSource, /RabbitMQ connector/);
  assert.match(releaseSource, /RabbitMQ is now a built-in connector with Direct and Over SSH connection modes/);
  assert.match(releaseSource, /Postgres over SSH/);
  assert.match(releaseSource, /Console profile polish/);
  assert.match(releaseSource, /Postgres management/);
  assert.match(releaseSource, /Maintenance hardening/);
  assert.match(releaseSource, /Updated golang\.org\/x\/crypto to 0\.53\.0/);
  assert.match(releaseSource, /Connector-native baseline/);
  assert.match(releaseSource, /SSH and Postgres now run through the same connector target/);
  assert.match(releaseSource, /Pre-0\.2 preview databases are not opened directly by the normal gateway/);
});
