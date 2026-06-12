import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";

import { buildMCPServerConfig, normalizeURL, parseFlags, sanitizeName, tomlKey, tomlString, writeJSONMCPConfig, writeProviderConfig } from "../src/init.js";
import { codexSkillPath, loadSkill, normalizeClient, renderInstruction, skillPathForClient } from "../src/install-skill.js";
import { normalizeLocalAPIURL } from "../src/local-url.js";

test("parseFlags supports kebab-case, inline values, and booleans", () => {
  assert.deepEqual(
    parseFlags(["--provider", "codex", "--api-url=http://localhost:3210/", "--name", "main", "--print", "--force"]),
    {
      provider: "codex",
      apiUrl: "http://localhost:3210/",
      name: "main",
      print: true,
      force: true,
    }
  );
  assert.deepEqual(parseFlags(["--token-stdin"]), { tokenStdin: true });
  assert.throws(() => parseFlags(["--token", "secret"]), /--token is not supported/);
});

test("sanitizeName keeps MCP-safe names", () => {
  assert.equal(sanitizeName(" aipermission default! "), "aipermission-default");
  assert.throws(() => sanitizeName("!!!"), /MCP server name is required/);
});

test("buildMCPServerConfig creates npx based bridge config", () => {
  assert.deepEqual(buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "TOKEN" }), {
    command: "npx",
    args: ["-y", "@aipermission/mcp"],
    env: {
      NODE_ENV: "production",
      AIPERMISSION_API_URL: "http://localhost:3210",
      AIPERMISSION_API_TOKEN: "TOKEN",
    },
  });
});

test("normalizeURL trims trailing slash", () => {
  assert.equal(normalizeURL("http://localhost:3210/"), "http://localhost:3210");
});

test("normalizeLocalAPIURL only accepts local gateway origins", () => {
  assert.equal(normalizeLocalAPIURL("http://127.0.0.1:3210"), "http://127.0.0.1:3210");
  assert.equal(normalizeLocalAPIURL("http://[::1]:3210/"), "http://[::1]:3210");
  assert.throws(() => normalizeLocalAPIURL("https://localhost:3210"), /http/);
  assert.throws(() => normalizeLocalAPIURL("http://example.com:3210"), /localhost/);
  assert.throws(() => normalizeLocalAPIURL("http://localhost:3210/api"), /origin only/);
});

test("toml helpers quote unsafe names and strings", () => {
  assert.equal(tomlKey("aipermission-default"), "aipermission-default");
  assert.equal(tomlKey("aipermission default"), "\"aipermission default\"");
  assert.equal(tomlString("TOKEN\nVALUE"), "\"TOKEN\\nVALUE\"");
});

test("writeJSONMCPConfig replaces invalid array root key with object", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-mcp-"));
  const filePath = path.join(dir, "mcp.json");
  await fs.writeFile(filePath, JSON.stringify({ mcpServers: [] }));

  await writeJSONMCPConfig(filePath, "aipermission", { command: "npx" }, "mcpServers");

  const parsed = JSON.parse(await fs.readFile(filePath, "utf8"));
  assert.deepEqual(parsed, { mcpServers: { aipermission: { command: "npx" } } });
});

test("writeProviderConfig writes Claude Code project MCP config", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-claude-"));
  try {
    process.chdir(dir);
    const result = await writeProviderConfig("claude-code", "aipermission", { command: "npx" });

    assert.equal(result.path, path.join(dir, ".mcp.json"));
    const parsed = JSON.parse(await fs.readFile(result.path, "utf8"));
    assert.deepEqual(parsed, { mcpServers: { aipermission: { command: "npx" } } });
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig adds project MCP configs to local git exclude", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-exclude-"));
  await fs.mkdir(path.join(dir, ".git", "info"), { recursive: true });
  await fs.writeFile(path.join(dir, ".git", "info", "exclude"), "# local excludes\n");
  try {
    process.chdir(dir);
    const result = await writeProviderConfig("cursor", "aipermission", { command: "npx" });

    assert.equal(result.path, path.join(dir, ".cursor", "mcp.json"));
    assert.equal(result.gitExcluded, true);
    assert.equal(result.gitExcludeEntry, ".cursor/mcp.json");
    const exclude = await fs.readFile(path.join(dir, ".git", "info", "exclude"), "utf8");
    assert.match(exclude, /^\.cursor\/mcp\.json$/m);
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig refuses to write tokens into tracked project MCP configs", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-tracked-"));
  const git = async (...args) => {
    const { spawn } = await import("node:child_process");
    await new Promise((resolve, reject) => {
      const child = spawn("git", args, { cwd: dir, stdio: "ignore" });
      child.on("error", reject);
      child.on("exit", (code) => {
        if (code === 0) {
          resolve();
          return;
        }
        reject(new Error(`git ${args.join(" ")} failed with ${code}`));
      });
    });
  };
  try {
    await git("init");
    await fs.writeFile(path.join(dir, ".mcp.json"), "{}\n");
    await git("add", ".mcp.json");
    process.chdir(dir);

    await assert.rejects(
      () => writeProviderConfig("claude-code", "aipermission", { command: "npx", env: { AIPERMISSION_API_TOKEN: "TOKEN" } }),
      /Refusing to write AIPERMISSION_API_TOKEN into tracked git file: \.mcp\.json/
    );

    const result = await writeProviderConfig("claude-code", "aipermission", { command: "npx" }, { force: true });
    assert.equal(result.path, path.join(dir, ".mcp.json"));
  } finally {
    process.chdir(previousCwd);
  }
});

test("codexSkillPath returns Codex skill location", () => {
  assert.equal(
    codexSkillPath("/home/alice"),
    "/home/alice/.codex/skills/aipermission-operator/SKILL.md"
  );
});

test("skillPathForClient maps clients to their documented instruction locations", () => {
  const options = { homeDir: "/home/alice", projectDir: "/repo" };
  assert.equal(skillPathForClient("codex", options), "/home/alice/.codex/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("claude", options), "/repo/.claude/rules/aipermission-operator.md");
  assert.equal(skillPathForClient("cursor", options), "/repo/.cursor/rules/aipermission-operator.mdc");
  assert.equal(skillPathForClient("vscode", options), "/repo/.github/instructions/aipermission-operator.instructions.md");
  assert.equal(skillPathForClient("windsurf", options), "/repo/.windsurf/rules/aipermission-operator.md");
  assert.equal(skillPathForClient("antigravity", options), "/repo/.agents/rules/aipermission-operator.md");
  assert.equal(skillPathForClient("gemini-cli", options), "/repo/GEMINI.md");
});

test("normalizeClient supports common aliases", () => {
  assert.equal(normalizeClient("claude"), "claude-code");
  assert.equal(normalizeClient("copilot"), "vscode");
  assert.equal(normalizeClient("agy"), "antigravity");
  assert.throws(() => normalizeClient("unknown"), /Unknown client/);
});

test("renderInstruction formats client-specific rule files", () => {
  const skill = "---\nname: aipermission-operator\n---\n# AIPermission Operator\n\nUse AIPermission safely.\n";

  assert.match(renderInstruction("cursor", skill), /alwaysApply: true/);
  assert.match(renderInstruction("vscode", skill), /applyTo: "\*\*"/);
  assert.match(renderInstruction("windsurf", skill), /trigger: always_on/);
  assert.match(renderInstruction("antigravity", skill), /description: AIPermission MCP operator workflow/);
  assert.match(renderInstruction("gemini", skill), /^## AIPermission Operator/);
  assert.doesNotMatch(renderInstruction("claude-code", skill), /name: aipermission-operator/);
});

test("loadSkill can read a local operator skill source", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-skill-"));
  const filePath = path.join(dir, "SKILL.md");
  await fs.writeFile(filePath, "---\nname: aipermission-operator\n---\n");

  const skill = await loadSkill(filePath);

  assert.match(skill, /name: aipermission-operator/);
});

test("loadSkill rejects remote HTTP sources", async () => {
  await assert.rejects(
    () => loadSkill("https://example.com/aipermission-operator/SKILL.md"),
    /remote skill sources are not supported/
  );
});

test("loadSkill reads the bundled operator skill by default", async () => {
  const skill = await loadSkill();

  assert.match(skill, /name: aipermission-operator/);
  assert.match(skill, /AIPermission Operator/);
});
