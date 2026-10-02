import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { buildMCPServerConfig, writeProviderConfig } from "../../src/init.js";
import * as init from "../../src/init.js";
import { git, initGitRepository } from "./git-fixtures.js";

test("Git protection escapes literal project paths and verifies all private paths", async (t) => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-init-git-literal-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  await initGitRepository(dir);
  const projectName = process.platform === "win32" ? "component [x] #!" : "component [x]*? #!";
  const projectDir = path.join(dir, projectName);
  await fs.mkdir(projectDir);
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "GIT_LITERAL_CANARY" });
  const result = await writeProviderConfig("cursor", "aipermission", config, { projectDir, scope: "project" });
  assert.equal(result.gitExcluded, true);
  assert.deepEqual(await init.inspectProjectConfigProtection(result.path, projectDir), {
    repository: true,
    relativePath: `${projectName}/.cursor/mcp.json`,
  });
  const exposedDir = path.join(projectDir, "exposed");
  await fs.mkdir(exposedDir);
  await assert.rejects(
    () => init.inspectProjectConfigProtection(path.join(exposedDir, "mcp.json"), projectDir),
    /Git still permits sensitive MCP path/,
  );
});

test("forced tracked config writes keep temporary paths protected and remain detectable", async (t) => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-init-force-"));
  t.after(() => fs.rm(dir, { recursive: true, force: true }));
  await initGitRepository(dir);
  const filePath = path.join(dir, ".mcp.json");
  await fs.writeFile(filePath, "{}\n");
  await git(dir, "add", ".mcp.json");
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "FORCE_CANARY" });
  const result = await writeProviderConfig("claude", "aipermission", config, { projectDir: dir, force: true });
  assert.equal(result.gitExcluded, true);
  assert.equal(JSON.parse(await fs.readFile(filePath, "utf8")).mcpServers.aipermission.env.AIPERMISSION_API_TOKEN, "FORCE_CANARY");
  for (const relativePath of [
    "..mcp.json.aipermission-crash.tmp",
    "..mcp.json.aipermission-stage-crash/.mcp.json",
    ".mcp.json.aipermission.lock",
  ]) {
    assert.equal(await git(dir, "check-ignore", "--", relativePath), relativePath);
  }
  await assert.rejects(() => init.inspectProjectConfigProtection(filePath, dir), /MCP config is tracked by Git/);
});
