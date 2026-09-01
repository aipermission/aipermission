import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { inspectClientSetup } from "../src/doctor.js";
import { buildMCPServerConfig, writeProviderConfig } from "../src/init.js";
import { installSkill } from "../src/install-skill.js";

test("doctor validates a complete JSON client setup without exposing secrets", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-json-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "DO_NOT_PRINT" });
  await writeProviderConfig("copilot", "aipermission", config, { homeDir, scope: "user" });
  await installSkill({ client: "copilot", homeDir, scope: "user" });

  const result = await inspectClientSetup({ client: "copilot", homeDir, scope: "user" });

  assert.equal(result.ok, true);
  assert.doesNotMatch(JSON.stringify(result), /DO_NOT_PRINT/);
});

test("doctor validates a complete TOML client setup", async () => {
  const projectDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-toml-"));
  const config = buildMCPServerConfig({ apiUrl: "http://127.0.0.1:3210", token: "SECRET" });
  await writeProviderConfig("grok", "main", config, { projectDir, scope: "project" });
  await installSkill({ client: "grok", projectDir, scope: "project" });

  const result = await inspectClientSetup({ client: "grok", name: "main", projectDir, scope: "project" });

  assert.equal(result.ok, true);
  assert.doesNotMatch(JSON.stringify(result), /SECRET/);
});

test("doctor reports missing config and skill paths", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-missing-"));

  const result = await inspectClientSetup({ client: "codex", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.deepEqual(
    result.checks.map((entry) => entry.ok),
    [false, false],
  );
  assert.match(result.checks[0].message, /not found/);
  assert.match(result.checks[1].message, /not found/);
});

test("doctor rejects a token config readable by other local users", { skip: process.platform === "win32" }, async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-mode-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "DO_NOT_PRINT" });
  const written = await writeProviderConfig("copilot", "aipermission", config, { homeDir, scope: "user" });
  await installSkill({ client: "copilot", homeDir, scope: "user" });
  await fs.chmod(written.path, 0o644);

  const result = await inspectClientSetup({ client: "copilot", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /chmod 600/);
  assert.doesNotMatch(JSON.stringify(result), /DO_NOT_PRINT/);
});
