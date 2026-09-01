import assert from "node:assert/strict";
import { execFile, spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import { createRequire } from "node:module";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { promisify } from "node:util";
import { parse as parseTOML } from "smol-toml";

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
import {
  codexSkillPath,
  installSkill,
  loadSkill,
  normalizeClient,
  renderInstruction,
  runInstallSkill,
  skillPathForClient,
  validateSkill,
} from "../src/install-skill.js";
import { normalizeLocalAPIURL } from "../src/local-url.js";
import { parseCommandFlags } from "../src/cli-flags.js";

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
  assert.deepEqual(parseFlags(["--print=false", "--force=false", "--token-stdin=false"]), {
    print: false,
    force: false,
    tokenStdin: false,
  });
  assert.throws(() => parseFlags(["--token", "secret"]), /--token is not supported/);
  assert.throws(() => parseFlags(["--install-skill"]), /Unknown init option/);
  assert.throws(() => parseFlags(["--scpoe", "project"]), /Unknown init option/);
  assert.throws(() => parseFlags(["--scope"]), /requires a non-empty value/);
  assert.throws(() => parseFlags(["--scope="]), /requires a non-empty value/);
  assert.throws(() => parseFlags(["unexpected"]), /does not accept positional argument/);
  assert.throws(() => parseFlags(["--print=maybe"]), /accepts only true or false/);
  assert.deepEqual(parseCommandFlags("setup", ["--skill-scope", "user", "--skill-source=SKILL.md"]), {
    skillScope: "user",
    skillSource: "SKILL.md",
  });
  assert.deepEqual(parseCommandFlags("doctor", ["--mcp-scope", "project", "--skill-scope", "user"]), {
    mcpScope: "project",
    skillScope: "user",
  });
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

test("writeJSONMCPConfig redacts malformed JSON parser context", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-mcp-malformed-"));
  const filePath = path.join(dir, "mcp.json");
  const canary = "CANARY_SUPER_SECRET_TOKEN";
  await fs.writeFile(filePath, `{"mcpServers":{"aipermission":{"env":{"AIPERMISSION_API_TOKEN":${canary}}}}}`);

  await assert.rejects(
    () => writeJSONMCPConfig(filePath, "aipermission", { command: "npx" }, "mcpServers"),
    (error) => {
      assert.match(error.message, /Could not parse JSON config/);
      assert.doesNotMatch(error.message, /CANARY|SUPER_SECRET/);
      return true;
    },
  );
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
    mcpServers: { aipermission: { command: "npx", type: "local", tools: ["*"] } },
  });
});

test("writeProviderConfig writes scoped Grok TOML config", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-grok-"));
  const config = { command: "npx", args: ["-y", "@aipermission/mcp@0.2.38"], env: { TOKEN: "secret" } };
  const result = await writeProviderConfig("grok", "aipermission", config, { homeDir: dir });

  assert.equal(result.path, path.join(dir, ".grok", "config.toml"));
  assert.match(await fs.readFile(result.path, "utf8"), /\[mcp_servers\.aipermission\]/);
  assert.match(await fs.readFile(result.path, "utf8"), /startup_timeout_sec = 60/);
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

test("writeTOMLMCPConfig handles quoted headers, comments, and unrelated multiline strings", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-toml-quoted-"));
  const filePath = path.join(dir, "config.toml");
  await fs.writeFile(
    filePath,
    '[theme]\nmessage = """\n[mcp_servers.aipermission]\nnot a real table\n"""\n\n[mcp_servers."aipermission"] # replace me\ncommand = "old"\n\n[mcp_servers.other] # preserve me\ncommand = "node"\n',
  );

  await writeTOMLMCPConfig(filePath, "aipermission", {
    command: "npx",
    args: ["-y", PACKAGE_SPECIFIER],
    env: { NODE_ENV: "production", AIPERMISSION_API_URL: "http://localhost:3210", AIPERMISSION_API_TOKEN: "TOKEN" },
  });

  const content = await fs.readFile(filePath, "utf8");
  assert.match(content, /not a real table/);
  assert.match(content, /\[mcp_servers\.other\] # preserve me/);
  const parsed = parseTOML(content);
  assert.equal(parsed.mcp_servers.aipermission.command, "npx");
  assert.doesNotMatch(content, /command = "old"/);
});

test("writeTOMLMCPConfig rejects malformed TOML without exposing parser context", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-toml-malformed-"));
  const filePath = path.join(dir, "config.toml");
  await fs.writeFile(filePath, 'AIPERMISSION_API_TOKEN = "TOML_CANARY_SECRET\n');
  await assert.rejects(
    () => writeTOMLMCPConfig(filePath, "aipermission", { command: "npx", args: [], env: {} }),
    (error) => {
      assert.match(error.message, /Could not parse TOML config/);
      assert.doesNotMatch(`${error.message}\n${error.cause?.message}\n${error.cause?.stack}`, /CANARY|SECRET/);
      return true;
    },
  );
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

test("writeProviderConfig rechecks Git tracking inside the config lock", async () => {
  const previousCwd = process.cwd();
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-git-race-"));
  try {
    await initGitRepository(dir);
    process.chdir(dir);
    let staged = false;
    await assert.rejects(
      () =>
        writeProviderConfig(
          "claude-code",
          "aipermission",
          { command: "npx", env: { AIPERMISSION_API_TOKEN: "TOKEN" } },
          {
            beforeWrite: async () => {
              if (staged) return;
              staged = true;
              await fs.writeFile(path.join(dir, ".mcp.json"), "{}\n");
              await git(dir, "add", "-f", ".mcp.json");
            },
          },
        ),
      /Refusing to write AIPERMISSION_API_TOKEN into tracked git file/,
    );
    assert.equal(await fs.readFile(path.join(dir, ".mcp.json"), "utf8"), "{}\n");
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
  assert.equal(codexSkillPath("/home/alice"), "/home/alice/.agents/skills/aipermission-operator/SKILL.md");
});

test("skillPathForClient maps clients to their native skill locations", () => {
  const options = { homeDir: "/home/alice", projectDir: "/repo" };
  assert.equal(skillPathForClient("codex", options).path, "/home/alice/.agents/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("claude", options).path, "/repo/.claude/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("cursor", options).path, "/repo/.cursor/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("vscode", options).path, "/repo/.github/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("copilot", options).path, "/home/alice/.copilot/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("windsurf", options).path, "/home/alice/.codeium/windsurf/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("antigravity", options).path, "/home/alice/.gemini/config/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("gemini-cli", options).path, "/home/alice/.gemini/skills/aipermission-operator/SKILL.md");
  assert.equal(skillPathForClient("grok", options).path, "/home/alice/.grok/skills/aipermission-operator/SKILL.md");
});

test("normalizeClient supports common aliases", () => {
  assert.equal(normalizeClient("claude"), "claude-code");
  assert.equal(normalizeClient("copilot"), "copilot");
  assert.equal(normalizeClient("agy"), "antigravity-cli");
  assert.throws(() => normalizeClient("unknown"), /Unknown client/);
});

test("renderInstruction preserves the canonical native skill", () => {
  const skill =
    "---\nname: aipermission-operator\ndescription: Operate AIPermission safely.\n---\n# AIPermission Operator\n\nUse AIPermission safely.\n";
  for (const client of [
    "codex",
    "claude-code",
    "cursor",
    "vscode",
    "copilot",
    "windsurf",
    "antigravity",
    "antigravity-cli",
    "gemini",
    "grok",
    "agents",
    "custom",
  ]) {
    assert.equal(renderInstruction(client, skill), skill);
  }
});

test("loadSkill can read a local operator skill source", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-skill-"));
  const filePath = path.join(dir, "SKILL.md");
  await fs.writeFile(filePath, "---\nname: aipermission-operator\ndescription: Test operator skill.\n---\n# Test\n");

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

test("runInstallSkill writes the canonical skill to the selected scope", async () => {
  const dir = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-native-skill-"));
  const source = path.join(dir, "source.md");
  const projectDir = path.join(dir, "project");
  const skill = "---\nname: aipermission-operator\ndescription: Test operator skill.\n---\n# AIPermission Operator\n";
  await fs.writeFile(source, skill);

  await runInstallSkill(["--client", "grok", "--scope", "project", "--project-dir", projectDir, "--source", source]);

  const installed = path.join(projectDir, ".grok", "skills", "aipermission-operator", "SKILL.md");
  assert.equal(await fs.readFile(installed, "utf8"), skill);
  assert.equal((await fs.stat(installed)).mode & 0o777, 0o644);
});

test("validateSkill rejects malformed, truncated, or empty native skills", () => {
  assert.throws(() => validateSkill("---\nname: aipermission-operator\n"), /closed YAML frontmatter/);
  assert.throws(() => validateSkill("---\nname: [\n---\n# Test\n"), /frontmatter is invalid/);
  assert.throws(() => validateSkill("---\nname: wrong\ndescription: Test.\n---\n# Test\n"), /exact skill name/);
  assert.throws(() => validateSkill("---\nname: aipermission-operator\n---\n# Test\n"), /include a description/);
  assert.throws(
    () => validateSkill("---\nname: aipermission-operator\ndescription: Test.\n---\n"),
    /include instructions after frontmatter/,
  );
});

for (const linkType of ["target", "parent"]) {
  test(`installSkill rejects a symbolic ${linkType}`, async () => {
    const root = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-skill-link-"));
    const projectDir = path.join(root, "project");
    const outside = path.join(root, "outside");
    await fs.mkdir(projectDir);
    await fs.mkdir(outside);
    const target = path.join(projectDir, ".agents", "skills", "aipermission-operator", "SKILL.md");
    if (linkType === "parent") {
      await fs.mkdir(path.dirname(path.dirname(path.dirname(target))), { recursive: true });
      await fs.symlink(outside, path.join(projectDir, ".agents", "skills"), "dir");
    } else {
      await fs.mkdir(path.dirname(target), { recursive: true });
      const outsideFile = path.join(outside, "SKILL.md");
      await fs.writeFile(outsideFile, "before");
      await fs.symlink(outsideFile, target);
    }

    await assert.rejects(() => installSkill({ client: "agents", scope: "project", projectDir }), /symbolic link|symbolic-link|junction/);
  });
}

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
