const assert = require("node:assert/strict");
const test = require("node:test");
const { readFileSync } = require("node:fs");
const { join } = require("node:path");

test("frontend MCP catalog is generated as a typed immutable snapshot of the canonical client registry", async () => {
  const { renderCatalog } = await import("../mcp-client-catalog.mjs");
  const { getClientCatalog } = await import("../../packages/mcp/src/client-registry.js");
  const source = readFileSync(join(__dirname, "../../frontend/src/lib/mcp-client-catalog.ts"), "utf8");
  assert.equal(source, renderCatalog(getClientCatalog()));
  assert.match(source, /\] as const\);/);
});

test("frontend client catalog only exposes client labels and supported capabilities", async () => {
  const { renderCatalog } = await import("../mcp-client-catalog.mjs");
  const source = renderCatalog([{ id: "fixture", label: 'A "Client"', supportsMCP: false, supportsSkill: true, credentials: "not-public" }]);
  assert.match(source, /id: "fixture"/);
  assert.match(source, /supportsMCP: false/);
  assert.match(source, /supportsSkill: true/);
  assert.ok(!source.includes("not-public"));
  assert.ok(!source.includes("credentials"));
});
