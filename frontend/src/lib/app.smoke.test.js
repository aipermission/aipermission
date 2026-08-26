import assert from "node:assert/strict";
import test from "node:test";
import {
  indexSource,
  themeInitSource,
  appSource,
  sidebarSource,
  releaseData,
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
  assert.match(releaseSource, /release\.generated\.json/);
  assert.equal(releaseData.version, releaseManifest.version);
  assert.equal(releaseData.entries.length, 8);
  assert.equal(releaseData.entries[0].version, releaseManifest.version);
  for (const entry of releaseData.entries) {
    assert.ok(entry.label);
    assert.ok(entry.sections.length > 0);
  }
});
