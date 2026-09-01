import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

import { mcpPackageName, mcpPackageSpecifier } from "./mcp-package.js";

const releaseManifest = JSON.parse(readFileSync(new URL("../../../release-manifest.json", import.meta.url), "utf8"));
const setupSource = readFileSync(new URL("../pages/mcp-setup.jsx", import.meta.url), "utf8");
const tokenInstallSource = readFileSync(new URL("../components/tokens/token-install-dialog.jsx", import.meta.url), "utf8");

test("manual MCP runtime configs use the release-pinned package specifier", () => {
  assert.equal(mcpPackageName, "@aipermission/mcp");
  assert.equal(mcpPackageSpecifier, `@aipermission/mcp@${releaseManifest.version}`);
  assert.match(setupSource, /args: \["-y", mcpPackageSpecifier\]/);
  assert.match(tokenInstallSource, /args: \["-y", mcpPackageSpecifier\]/);
  assert.doesNotMatch(setupSource, /args: \["-y", mcpPackageName\]/);
  assert.doesNotMatch(tokenInstallSource, /args: \["-y", mcpPackageName\]/);
});

test("recommended MCP install commands use setup so config and skill stay aligned", () => {
  assert.match(setupSource, /mcpPackageName} setup/);
  assert.match(tokenInstallSource, /mcpPackageName} setup/);
  assert.doesNotMatch(setupSource, /mcpPackageName} init/);
  assert.doesNotMatch(tokenInstallSource, /mcpPackageName} init/);
});
