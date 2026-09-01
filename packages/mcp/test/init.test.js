import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import fs from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";

import {
  assertProviderSelectionAvailable,
  buildMCPServerConfig,
  normalizeURL,
  PACKAGE_SPECIFIER,
  parseFlags,
  sanitizeName,
  tomlKey,
  tomlString,
  writeTOMLMCPConfig,
  writeJSONMCPConfig,
  writeProviderConfig,
} from "../src/init.js";
import { codexSkillPath, loadSkill, normalizeClient, renderInstruction, skillPathForClient } from "../src/install-skill.js";
import { normalizeLocalAPIURL } from "../src/local-url.js";

const require = createRequire(import.meta.url);
const packageMetadata = require("../package.json");
const execFileAsync = promisify(execFile);

async function git(cwd, ...args) {
  const { stdout } = await execFileAsync("git", ["-C", cwd, ...args], { encoding: "utf8", windowsHide: true });
  return stdout.trim();
}

async function initGitRepository(dir) {
  await git(dir, "init");
  await git(dir, "config", "user.name", "AIPermission Tests");
  await git(dir, "config", "user.email", "tests@aipermission.local");
}

test("parseFlags supports kebab-case, inline values, and booleans", () => {
  assert.deepEqual(parseFlags(["--provider", "codex", "--api-url=http://localhost:3210/", "--name", "main", "--print", "--force"]), {
    provider: "codex",
    apiUrl: "http://localhost:3210/",
    name: "main",
    print: true,
    force: true,
  });
  assert.deepEqual(parseFlags(["--token-stdin"]), { tokenStdin: true });
  assert.throws(() => parseFlags(["--token", "secret"]), /--token is not supported/);
});

test("non-interactive init requires an explicit provider", () => {
  assert.throws(() => assertProviderSelectionAvailable("", false), /requires an explicit --provider/);
  assert.doesNotThrow(() => assertProviderSelectionAvailable("codex", false));
  assert.doesNotThrow(() => assertProviderSelectionAvailable("", true));
});

test("sanitizeName keeps MCP-safe names", () => {
  assert.equal(sanitizeName(" aipermission default! "), "aipermission-default");
  assert.throws(() => sanitizeName("!!!"), /MCP server name is required/);
  for (const reserved of ["__proto__", "prototype", "constructor"]) {
    assert.throws(() => sanitizeName(reserved), /MCP server name is reserved/);
  }
});

test("buildMCPServerConfig creates npx based bridge config", () => {
  assert.equal(PACKAGE_SPECIFIER, `@aipermission/mcp@${packageMetadata.version}`);
  assert.match(PACKAGE_SPECIFIER, /^@aipermission\/mcp@\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/);
  assert.deepEqual(buildMCPServerConfig({ apiUrl: "http://localhost:3210", token: "TOKEN" }), {
    command: "npx",
    args: ["-y", PACKAGE_SPECIFIER],
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
  assert.equal(tomlKey("aipermission default"), '"aipermission default"');
  assert.equal(tomlString("TOKEN\nVALUE"), '"TOKEN\\nVALUE"');
});

test("writeJSONMCPConfig replaces invalid array root key with object", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-mcp-"));
  const filePath = path.join(dir, "mcp.json");
  await fs.writeFile(filePath, JSON.stringify({ mcpServers: [] }));

  await writeJSONMCPConfig(filePath, "aipermission", { command: "npx" }, "mcpServers");

  const parsed = JSON.parse(await fs.readFile(filePath, "utf8"));
  assert.deepEqual(parsed, { mcpServers: { aipermission: { command: "npx" } } });
});

test("writeJSONMCPConfig serializes concurrent read-modify-write updates", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-mcp-concurrent-"));
  const filePath = path.join(dir, "mcp.json");

  await Promise.all([
    writeJSONMCPConfig(filePath, "first", { command: "one" }, "mcpServers"),
    writeJSONMCPConfig(filePath, "second", { command: "two" }, "mcpServers"),
  ]);

  assert.deepEqual(JSON.parse(await fs.readFile(filePath, "utf8")), {
    mcpServers: { first: { command: "one" }, second: { command: "two" } },
  });
});

test("writeProviderConfig writes Claude Code project MCP config", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-claude-"));
  try {
    process.chdir(dir);
    const result = await writeProviderConfig("claude-code", "aipermission", { command: "npx" });

    assert.equal(result.path, path.join(dir, ".mcp.json"));
    assert.equal(result.gitExcluded, undefined);
    assert.equal(result.scope, "project");
    const parsed = JSON.parse(await fs.readFile(result.path, "utf8"));
    assert.deepEqual(parsed, { mcpServers: { aipermission: { command: "npx" } } });
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig writes user-scoped Copilot CLI config", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-copilot-"));
  const result = await writeProviderConfig("copilot", "aipermission", { command: "npx" }, { homeDir: dir });

  assert.equal(result.path, path.join(dir, ".copilot", "mcp-config.json"));
  assert.equal(result.scope, "user");
  assert.deepEqual(JSON.parse(await fs.readFile(result.path, "utf8")), {
    mcpServers: { aipermission: { command: "npx" } },
  });
});

test("writeProviderConfig writes scoped Grok TOML config", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-grok-"));
  const config = { command: "npx", args: ["-y", "@aipermission/mcp@0.2.38"], env: { TOKEN: "secret" } };
  const result = await writeProviderConfig("grok", "aipermission", config, { homeDir: dir });

  assert.equal(result.path, path.join(dir, ".grok", "config.toml"));
  assert.match(await fs.readFile(result.path, "utf8"), /\[mcp_servers\.aipermission\]/);
});

test("writeTOMLMCPConfig replaces only the selected MCP server", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-toml-"));
  const filePath = path.join(dir, "config.toml");
  await fs.writeFile(
    filePath,
    '[theme]\nname = "dark"\n\n[mcp_servers.other]\ncommand = "node"\n\n[mcp_servers.aipermission]\ncommand = "old"\n\n[mcp_servers.aipermission.extra]\nvalue = "remove-me"\n',
  );

  await writeTOMLMCPConfig(filePath, "aipermission", { command: "npx", args: [], env: {} });
  await writeTOMLMCPConfig(filePath, "aipermission", { command: "node", args: ["server.js"], env: {} });

  const content = await fs.readFile(filePath, "utf8");
  assert.match(content, /\[theme\]/);
  assert.match(content, /\[mcp_servers\.other\]/);
  assert.equal(content.match(/\[mcp_servers\.aipermission\]/g)?.length, 1);
  assert.match(content, /command = "node"/);
  assert.doesNotMatch(content, /remove-me/);
});

test("writeProviderConfig adds project MCP configs to local git exclude", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-exclude-"));
  await initGitRepository(dir);
  const gitDir = await git(dir, "rev-parse", "--absolute-git-dir");
  await fs.writeFile(path.join(gitDir, "info", "exclude"), "# local excludes\n");
  try {
    process.chdir(dir);
    const result = await writeProviderConfig("cursor", "aipermission", { command: "npx" });

    assert.equal(result.path, path.join(dir, ".cursor", "mcp.json"));
    assert.equal(result.gitExcluded, true);
    assert.equal(result.gitExcludeEntry, ".cursor/mcp.json");
    assert.equal(result.gitExcludeTemporaryEntry, ".cursor/.mcp.json.aipermission-*.tmp");
    assert.equal(result.gitExcludeStagingEntry, ".cursor/.mcp.json.aipermission-stage-*");
    assert.equal(result.gitExcludeLockEntry, ".cursor/mcp.json.aipermission.lock");
    const exclude = await fs.readFile(path.join(gitDir, "info", "exclude"), "utf8");
    assert.match(exclude, /^\/\.cursor\/mcp\.json$/m);
    assert.match(exclude, /^\/\.cursor\/\.mcp\.json\.aipermission-\*\.tmp$/m);
    assert.match(exclude, /^\/\.cursor\/\.mcp\.json\.aipermission-stage-\*$/m);
    assert.match(exclude, /^\/\.cursor\/mcp\.json\.aipermission\.lock$/m);
    const simulatedCrashTemp = path.join(dir, ".cursor", ".mcp.json.aipermission-crash.tmp");
    await fs.writeFile(simulatedCrashTemp, "TOKEN");
    assert.equal(
      await git(dir, "check-ignore", "--", ".cursor/.mcp.json.aipermission-crash.tmp"),
      ".cursor/.mcp.json.aipermission-crash.tmp",
    );
    const simulatedCrashStage = path.join(dir, ".cursor", ".mcp.json.aipermission-stage-crash", "mcp.json");
    await fs.mkdir(path.dirname(simulatedCrashStage));
    await fs.writeFile(simulatedCrashStage, "TOKEN");
    assert.equal(
      await git(dir, "check-ignore", "--", ".cursor/.mcp.json.aipermission-stage-crash/mcp.json"),
      ".cursor/.mcp.json.aipermission-stage-crash/mcp.json",
    );
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig fails before writing when local Git exclusion cannot be read", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-exclude-failure-"));
  await initGitRepository(dir);
  const gitDir = await git(dir, "rev-parse", "--absolute-git-dir");
  await fs.rm(path.join(gitDir, "info", "exclude"), { force: true });
  await fs.mkdir(path.join(gitDir, "info", "exclude"));
  try {
    process.chdir(dir);
    await assert.rejects(
      () => writeProviderConfig("claude-code", "aipermission", { command: "npx" }),
      /Could not protect MCP config with local Git excludes/,
    );
    await assert.rejects(() => fs.stat(path.join(dir, ".mcp.json")), { code: "ENOENT" });
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig refuses to write tokens into tracked project MCP configs", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-tracked-"));
  try {
    await initGitRepository(dir);
    await fs.writeFile(path.join(dir, ".mcp.json"), "{}\n");
    await git(dir, "add", ".mcp.json");
    process.chdir(dir);

    await assert.rejects(
      () => writeProviderConfig("claude-code", "aipermission", { command: "npx", env: { AIPERMISSION_API_TOKEN: "TOKEN" } }),
      /Refusing to write AIPERMISSION_API_TOKEN into tracked git file: \.mcp\.json/,
    );

    const result = await writeProviderConfig("claude-code", "aipermission", { command: "npx" }, { force: true });
    assert.equal(result.path, path.join(dir, ".mcp.json"));
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig protects configs in linked worktrees", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-worktree-source-"));
  const worktree = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-worktree-linked-"));
  await fs.rm(worktree, { recursive: true });
  try {
    await initGitRepository(dir);
    await fs.writeFile(path.join(dir, "README.md"), "test\n");
    await git(dir, "add", "README.md");
    await git(dir, "commit", "-m", "initial");
    await git(dir, "worktree", "add", "-b", "linked-test", worktree);
    process.chdir(worktree);

    const result = await writeProviderConfig("cursor", "aipermission", { command: "npx" });

    const excludePath = await git(worktree, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude");
    const exclude = await fs.readFile(excludePath, "utf8");
    assert.equal(result.gitExcluded, true);
    assert.match(exclude, /^\/\.cursor\/mcp\.json$/m);
  } finally {
    process.chdir(previousCwd);
    await git(dir, "worktree", "remove", "--force", worktree).catch(() => {});
  }
});

test("writeProviderConfig protects configs inside submodules", async () => {
  const previousCwd = process.cwd();
  const parent = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-submodule-parent-"));
  const source = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-submodule-source-"));
  try {
    await initGitRepository(source);
    await fs.writeFile(path.join(source, "README.md"), "submodule\n");
    await git(source, "add", "README.md");
    await git(source, "commit", "-m", "initial");
    await initGitRepository(parent);
    await fs.writeFile(path.join(parent, "README.md"), "parent\n");
    await git(parent, "add", "README.md");
    await git(parent, "commit", "-m", "initial");
    await git(parent, "-c", "protocol.file.allow=always", "submodule", "add", source, "vendor/component");
    const submodule = path.join(parent, "vendor", "component");
    process.chdir(submodule);

    const result = await writeProviderConfig("cursor", "aipermission", { command: "npx" });

    const gitDir = await git(submodule, "rev-parse", "--absolute-git-dir");
    const exclude = await fs.readFile(path.join(gitDir, "info", "exclude"), "utf8");
    assert.equal(result.gitExcluded, true);
    assert.match(exclude, /^\/\.cursor\/mcp\.json$/m);
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig serializes concurrent local Git exclude updates", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-concurrent-"));
  await initGitRepository(dir);
  try {
    process.chdir(dir);
    await Promise.all([
      writeProviderConfig("cursor", "cursor-test", { command: "one" }),
      writeProviderConfig("vscode", "vscode-test", { command: "two" }),
    ]);
    const excludePath = await git(dir, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude");
    const exclude = await fs.readFile(excludePath, "utf8");
    assert.match(exclude, /^\/\.cursor\/mcp\.json$/m);
    assert.match(exclude, /^\/\.vscode\/mcp\.json$/m);
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig fails closed when Git index inspection fails", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-corrupt-"));
  await initGitRepository(dir);
  await fs.writeFile(path.join(dir, ".git", "index"), "not a git index");
  try {
    process.chdir(dir);
    await assert.rejects(
      () => writeProviderConfig("claude-code", "aipermission", { command: "npx" }),
      /Could not verify whether MCP config is tracked by Git/,
    );
    await assert.rejects(() => fs.stat(path.join(dir, ".mcp.json")), { code: "ENOENT" });
  } finally {
    process.chdir(previousCwd);
  }
});

test("writeProviderConfig fails closed when a higher-precedence rule exposes the config", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-negation-"));
  await initGitRepository(dir);
  await fs.writeFile(path.join(dir, ".gitignore"), "!.cursor/mcp.json\n");
  try {
    process.chdir(dir);
    await assert.rejects(() => writeProviderConfig("cursor", "aipermission", { command: "npx" }), /Git still permits sensitive MCP path/);
    await assert.rejects(() => fs.stat(path.join(dir, ".cursor", "mcp.json")), { code: "ENOENT" });
  } finally {
    process.chdir(previousCwd);
  }
});

test("codexSkillPath returns Codex skill location", () => {
  assert.equal(codexSkillPath("/home/alice"), "/home/alice/.codex/skills/aipermission-operator/SKILL.md");
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
  assert.equal(normalizeClient("copilot"), "copilot");
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
  await assert.rejects(() => loadSkill("https://example.com/aipermission-operator/SKILL.md"), /remote skill sources are not supported/);
});

test("loadSkill reads the bundled operator skill by default", async () => {
  const skill = await loadSkill();

  assert.match(skill, /name: aipermission-operator/);
  assert.match(skill, /AIPermission Operator/);
});
