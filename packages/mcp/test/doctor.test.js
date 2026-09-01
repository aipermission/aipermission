import assert from "node:assert/strict";
import { execFile, spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import { inspectClientSetup } from "../src/doctor.js";
import { buildMCPServerConfig, writeProviderConfig } from "../src/init.js";
import { installSkill } from "../src/install-skill.js";

const execFileAsync = promisify(execFile);

async function git(cwd, ...args) {
  await execFileAsync("git", ["-C", cwd, ...args], { encoding: "utf8", windowsHide: true });
}

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

test("doctor rejects decoy package arguments", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-decoy-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "SECRET" });
  config.args = ["-y", "malicious-package", config.args[1]];
  await writeProviderConfig("cursor", "aipermission", config, { homeDir, scope: "user" });
  await installSkill({ client: "cursor", homeDir, scope: "user" });

  const result = await inspectClientSetup({ client: "cursor", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /exact .* command arguments/);
});

test("doctor redacts malformed JSON parser context", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-malformed-"));
  const configPath = path.join(homeDir, ".cursor", "mcp.json");
  await fs.mkdir(path.dirname(configPath), { recursive: true });
  await fs.writeFile(configPath, '{"mcpServers":{"aipermission":{"env":{"AIPERMISSION_API_TOKEN":DOCTOR_CANARY_SECRET}}}}', {
    mode: 0o600,
  });
  await installSkill({ client: "cursor", homeDir, scope: "user" });

  const result = await inspectClientSetup({ client: "cursor", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /JSON parsing failed/);
  assert.doesNotMatch(JSON.stringify(result), /CANARY|SECRET/);
});

test("doctor CLI never writes malformed config secrets to stdout or stderr", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-cli-redaction-"));
  const configPath = path.join(homeDir, ".cursor", "mcp.json");
  await fs.mkdir(path.dirname(configPath), { recursive: true });
  await fs.writeFile(configPath, '{"mcpServers":{"aipermission":{"env":{"AIPERMISSION_API_TOKEN":CLI_DOCTOR_CANARY_SECRET}}}}', {
    mode: 0o600,
  });
  await installSkill({ client: "cursor", homeDir, scope: "user" });

  const result = spawnSync(
    process.execPath,
    [path.resolve("src/cli.js"), "doctor", "--client", "cursor", "--home", homeDir, "--scope", "user"],
    { encoding: "utf8" },
  );

  assert.notEqual(result.status, 0);
  assert.match(result.stdout, /JSON parsing failed/);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, /CANARY|SECRET/);
});

test("doctor rejects truncated native skill frontmatter", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-skill-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "SECRET" });
  await writeProviderConfig("cursor", "aipermission", config, { homeDir, scope: "user" });
  const skillPath = path.join(homeDir, ".cursor", "skills", "aipermission-operator", "SKILL.md");
  await fs.mkdir(path.dirname(skillPath), { recursive: true });
  await fs.writeFile(skillPath, "---\nname: aipermission-operator\n");

  const result = await inspectClientSetup({ client: "cursor", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.match(result.checks[1].message, /closed YAML frontmatter/);
});

test("doctor rejects config symlinks", async () => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-link-"));
  const homeDir = path.join(root, "home");
  const outside = path.join(root, "outside.json");
  await fs.mkdir(path.join(homeDir, ".cursor"), { recursive: true });
  await fs.writeFile(outside, "{}\n", { mode: 0o600 });
  await fs.symlink(outside, path.join(homeDir, ".cursor", "mcp.json"));
  await installSkill({ client: "cursor", homeDir, scope: "user" });

  const result = await inspectClientSetup({ client: "cursor", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /symbolic-link/);
});

test("doctor rejects tracked project token configs", async () => {
  const projectDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-tracked-"));
  await git(projectDir, "init");
  await git(projectDir, "config", "user.name", "AIPermission Tests");
  await git(projectDir, "config", "user.email", "tests@aipermission.local");
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "SECRET" });
  await writeProviderConfig("cursor", "aipermission", config, { projectDir, scope: "project" });
  await installSkill({ client: "cursor", projectDir, scope: "project" });
  await git(projectDir, "add", "-f", ".cursor/mcp.json");

  const result = await inspectClientSetup({ client: "cursor", projectDir, scope: "project" });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /tracked by Git/);
});

test("doctor supports separate MCP and skill scopes", async () => {
  const projectDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-split-project-"));
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-split-home-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "SECRET" });
  await writeProviderConfig("vscode", "aipermission", config, { projectDir, scope: "project" });
  await installSkill({ client: "vscode", homeDir, scope: "user" });

  const result = await inspectClientSetup({
    client: "vscode",
    mcpScope: "project",
    skillScope: "user",
    homeDir,
    projectDir,
  });

  assert.equal(result.ok, true);
  assert.equal(result.scope, "MCP project, skill user");
});

test("doctor rejects missing client-specific MCP fields", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-client-fields-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "SECRET" });
  const written = await writeProviderConfig("copilot", "aipermission", config, { homeDir, scope: "user" });
  await installSkill({ client: "copilot", homeDir, scope: "user" });
  const root = JSON.parse(await fs.readFile(written.path, "utf8"));
  delete root.mcpServers.aipermission.tools;
  await fs.writeFile(written.path, `${JSON.stringify(root, null, 2)}\n`, { mode: 0o600 });

  const result = await inspectClientSetup({ client: "copilot", homeDir, scope: "user" });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /invalid tools field/);
});

test("doctor validates Windows ACLs and fails closed on inherited permissions", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-doctor-windows-acl-"));
  const config = buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "SECRET" });
  await writeProviderConfig("cursor", "aipermission", config, { homeDir, scope: "user" });
  await installSkill({ client: "cursor", homeDir, scope: "user" });
  const sid = "S-1-5-21-1000";
  const windowsExec = async (executable) => {
    if (executable.endsWith("whoami.exe")) return { stdout: `"user","${sid}"\n`, stderr: "" };
    if (executable.endsWith("icacls.exe")) return { stdout: `config ${sid}:(I)(F)\n`, stderr: "" };
    throw new Error(`Unexpected executable: ${executable}`);
  };

  const result = await inspectClientSetup({
    client: "cursor",
    homeDir,
    scope: "user",
    platform: "win32",
    execFile: windowsExec,
  });

  assert.equal(result.ok, false);
  assert.match(result.checks[0].message, /Windows ACL is not restricted/);
});
