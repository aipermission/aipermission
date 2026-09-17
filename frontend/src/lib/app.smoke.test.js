import assert from "node:assert/strict";
import test from "node:test";
import { runInNewContext } from "node:vm";
import {
  indexSource,
  themeInitSource,
  appSource,
  sidebarSource,
  releaseData,
  releaseSource,
  releaseManifest,
} from "./smoke/app-smoke-fixtures.test.js";

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
});

test("static bootstrap and release metadata stay bundled", () => {
  assert.match(indexSource, /<script src="\/theme-init\.js"><\/script>/);
  assert.doesNotMatch(indexSource, /localStorage\.getItem\("aipermission-theme"\)/);
  assert.match(themeInitSource, /localStorage\.getItem\("aipermission-theme"\)/);
  assert.match(releaseSource, /release\.generated\.json/);
  assert.equal(releaseData.version, releaseManifest.version);
  assert.equal(releaseData.entries.length, 8);
  assert.equal(releaseData.entries[0].version, releaseManifest.version);
  for (const entry of releaseData.entries) {
    assert.ok(entry.label);
    assert.ok(entry.sections.length > 0);
  }
});

test("theme bootstrap tolerates blocked storage and rejects unknown values", () => {
  for (const [stored, expected] of [
    ["light", "light"],
    ["unknown", "dark"],
  ]) {
    const document = { documentElement: { dataset: {} } };
    runInNewContext(themeInitSource, { document, localStorage: { getItem: () => stored } });
    assert.equal(document.documentElement.dataset.theme, expected);
  }

  const document = { documentElement: { dataset: {} } };
  runInNewContext(themeInitSource, {
    document,
    localStorage: {
      getItem() {
        throw new DOMException("Storage denied", "SecurityError");
      },
    },
    DOMException,
  });
  assert.equal(document.documentElement.dataset.theme, "dark");
});
