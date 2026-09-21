import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";
import { connectorActionStatuses } from "../src/generated-connector-contract.js";

const testDir = path.dirname(fileURLToPath(import.meta.url));
const packageRoot = path.resolve(testDir, "..");
const repoRoot = path.resolve(packageRoot, "..", "..");

test("bundled operator skill matches canonical docs skill", async () => {
  const canonical = await fs.readFile(path.join(repoRoot, "docs", "skills", "aipermission-operator", "SKILL.md"), "utf8");
  const bundled = await fs.readFile(path.join(packageRoot, "resources", "aipermission-operator", "SKILL.md"), "utf8");
  assert.equal(bundled, canonical);
});

test("operator skill terminal statuses match the connector action contract", async () => {
  const canonical = await fs.readFile(path.join(repoRoot, "docs", "skills", "aipermission-operator", "SKILL.md"), "utf8");
  const match = canonical.match(/Terminal statuses:\s*```text\s*([\s\S]*?)```/);
  assert.ok(match, "operator skill must declare terminal statuses");
  const documented = match[1]
    .split(/\s+/)
    .map((status) => status.trim())
    .filter(Boolean)
    .sort();
  const expected = connectorActionStatuses.filter((status) => !["approval_pending", "running"].includes(status)).sort();
  assert.deepEqual(documented, expected);
});

test("package MCP registry name matches server metadata", async () => {
  const packageJSON = JSON.parse(await fs.readFile(path.join(packageRoot, "package.json"), "utf8"));
  const serverJSON = JSON.parse(await fs.readFile(path.join(packageRoot, "server.json"), "utf8"));
  assert.equal(packageJSON.mcpName, serverJSON.name);
  assert.equal(serverJSON.packages[0].identifier, packageJSON.name);
  assert.equal(serverJSON.packages[0].version, packageJSON.version);
});
