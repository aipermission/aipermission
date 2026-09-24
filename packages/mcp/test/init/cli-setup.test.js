import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

test("CLI rejects init-only flag drift and honors explicit false booleans", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-cli-flags-"));
  const cliPath = path.resolve("src/cli.js");
  const invalid = spawnSync(process.execPath, [cliPath, "init", "--provider", "codex", "--install-skill"], {
    encoding: "utf8",
  });
  assert.notEqual(invalid.status, 0);
  assert.match(invalid.stderr, /Unknown init option: --install-skill/);

  const token = "CLI_FALSE_CANARY_TOKEN";
  const result = spawnSync(
    process.execPath,
    [cliPath, "init", "--provider", "codex", "--home", homeDir, "--token-stdin=false", "--print=false", "--force=false"],
    { encoding: "utf8", input: `${token}\n` },
  );
  assert.equal(result.status, 0, result.stderr);
  assert.doesNotMatch(result.stdout, new RegExp(token));
  assert.match(await fs.readFile(path.join(homeDir, ".codex", "config.toml"), "utf8"), new RegExp(token));
});

test("setup preflights the skill before writing a token config", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-setup-preflight-"));
  const invalidSkill = path.join(homeDir, "invalid-skill.md");
  await fs.writeFile(invalidSkill, "---\nname: aipermission-operator\n");
  const result = spawnSync(
    process.execPath,
    [path.resolve("src/cli.js"), "setup", "--provider", "copilot", "--home", homeDir, "--token-stdin", "--skill-source", invalidSkill],
    { encoding: "utf8", input: "SETUP_CANARY_TOKEN\n" },
  );
  assert.notEqual(result.status, 0);
  await assert.rejects(() => fs.stat(path.join(homeDir, ".copilot", "mcp-config.json")), { code: "ENOENT" });
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, /SETUP_CANARY_TOKEN/);
});

test("setup leaves skill unchanged when VS Code config cannot be updated", async () => {
  const projectDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-vscode-setup-invalid-"));
  const configPath = path.join(projectDir, ".vscode", "mcp.json");
  await fs.mkdir(path.dirname(configPath), { recursive: true });
  await fs.writeFile(configPath, '{ "servers": { broken } }');
  const result = spawnSync(
    process.execPath,
    [path.resolve("src/cli.js"), "setup", "--provider", "vscode", "--project-dir", projectDir, "--token-stdin"],
    { encoding: "utf8", input: "SETUP_CANARY_TOKEN\n" },
  );
  assert.notEqual(result.status, 0);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, /SETUP_CANARY_TOKEN/);
  assert.equal(await fs.readFile(configPath, "utf8"), '{ "servers": { broken } }');
  await assert.rejects(() => fs.stat(path.join(projectDir, ".github", "skills", "aipermission-operator", "SKILL.md")), {
    code: "ENOENT",
  });
});

test("setup reads a piped token before asynchronous skill preflight", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-setup-stdin-"));
  const skillSource = path.join(homeDir, "operator-skill.md");
  await fs.writeFile(skillSource, "---\nname: aipermission-operator\ndescription: Test operator skill.\n---\n# AIPermission Operator\n");
  const token = "SETUP_STDIN_CANARY_TOKEN";
  const result = spawnSync(
    process.execPath,
    [path.resolve("src/cli.js"), "setup", "--provider", "codex", "--home", homeDir, "--token-stdin", "--skill-source", skillSource],
    { encoding: "utf8", input: `${token}\n` },
  );

  assert.equal(result.status, 0, result.stderr);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, new RegExp(token));
  assert.match(await fs.readFile(path.join(homeDir, ".codex", "config.toml"), "utf8"), new RegExp(token));
  assert.match(
    await fs.readFile(path.join(homeDir, ".agents", "skills", "aipermission-operator", "SKILL.md"), "utf8"),
    /Test operator skill/,
  );
});

test("CLI print emits the selected provider format and validates scope", () => {
  const cliPath = path.resolve("src/cli.js");
  const run = (provider, scope) =>
    spawnSync(process.execPath, [cliPath, "init", "--provider", provider, "--scope", scope, "--print", "--token-stdin"], {
      encoding: "utf8",
      input: "PRINT_CANARY_TOKEN\n",
    });

  const codex = run("codex", "user");
  assert.equal(codex.status, 0, codex.stderr);
  assert.match(codex.stdout, /\[mcp_servers\.aipermission\]/);
  assert.match(codex.stdout, /YOUR_TOKEN_HERE/);
  assert.doesNotMatch(codex.stdout, /PRINT_CANARY_TOKEN/);
  assert.doesNotMatch(codex.stdout, /"mcpServers"/);

  const vscode = run("vscode", "user");
  assert.equal(vscode.status, 0, vscode.stderr);
  assert.match(vscode.stdout, /"servers"/);
  assert.match(vscode.stdout, /YOUR_TOKEN_HERE/);
  assert.doesNotMatch(vscode.stdout, /PRINT_CANARY_TOKEN/);
  assert.doesNotMatch(vscode.stdout, /"mcpServers"/);

  const custom = run("custom", "user");
  assert.equal(custom.status, 0, custom.stderr);
  assert.match(custom.stdout, /"mcpServers"/);
  assert.match(custom.stdout, /YOUR_TOKEN_HERE/);
  assert.doesNotMatch(custom.stdout, /PRINT_CANARY_TOKEN/);

  const unsupported = run("windsurf", "project");
  assert.notEqual(unsupported.status, 0);
  assert.match(unsupported.stderr, /does not support project MCP config scope/);
});

test("setup --print has no filesystem side effects", async () => {
  const homeDir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-setup-print-"));
  const result = spawnSync(
    process.execPath,
    [path.resolve("src/cli.js"), "setup", "--provider", "codex", "--scope", "user", "--home", homeDir, "--print", "--token-stdin"],
    { encoding: "utf8", input: "PRINT_ONLY_CANARY_TOKEN\n" },
  );

  assert.equal(result.status, 0, result.stderr);
  assert.match(result.stdout, /\[mcp_servers\.aipermission\]/);
  assert.match(result.stdout, /YOUR_TOKEN_HERE/);
  assert.doesNotMatch(`${result.stdout}\n${result.stderr}`, /PRINT_ONLY_CANARY_TOKEN/);
  assert.match(result.stderr, /No files were changed/);
  await assert.rejects(() => fs.stat(path.join(homeDir, ".codex", "config.toml")), { code: "ENOENT" });
  await assert.rejects(() => fs.stat(path.join(homeDir, ".agents", "skills", "aipermission-operator", "SKILL.md")), {
    code: "ENOENT",
  });
});
