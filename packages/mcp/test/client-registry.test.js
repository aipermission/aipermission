import assert from "node:assert/strict";
import test from "node:test";

import {
  adaptMCPServerConfig,
  clientLabel,
  getClient,
  MCP_PROVIDERS,
  normalizeClientID,
  normalizeScope,
  resolveMCPConfigTarget,
  resolveMCPPrintTarget,
  resolveSkillTarget,
} from "../src/client-registry.js";

test("client registry exposes MCP setup providers", () => {
  assert.deepEqual(
    MCP_PROVIDERS.map((provider) => provider.id),
    ["codex", "claude-code", "cursor", "vscode", "copilot", "windsurf", "antigravity", "antigravity-cli", "gemini", "grok", "custom"],
  );
  assert.equal(getClient("OpenAI Codex").id, "codex");
});

test("client registry normalizes aliases from one source", () => {
  assert.equal(normalizeClientID("claude"), "claude-code");
  assert.equal(normalizeClientID("copilot"), "copilot");
  assert.equal(normalizeClientID("agy"), "antigravity-cli");
  assert.equal(clientLabel("vscode"), "VS Code");
  assert.throws(() => normalizeClientID("unknown"), /Unknown client/);
});

test("client registry resolves verified MCP targets by scope", () => {
  const roots = { homeDir: "/home/alice", projectDir: "/repo" };
  const expected = {
    codex: { user: "/home/alice/.codex/config.toml", project: "/repo/.codex/config.toml", defaultScope: "user" },
    "claude-code": { user: "/home/alice/.claude.json", project: "/repo/.mcp.json", defaultScope: "project" },
    cursor: { user: "/home/alice/.cursor/mcp.json", project: "/repo/.cursor/mcp.json", defaultScope: "project" },
    vscode: { project: "/repo/.vscode/mcp.json", defaultScope: "project" },
    copilot: { user: "/home/alice/.copilot/mcp-config.json", project: "/repo/.mcp.json", defaultScope: "user" },
    windsurf: { user: "/home/alice/.codeium/windsurf/mcp_config.json", defaultScope: "user" },
    antigravity: {
      user: "/home/alice/.gemini/config/mcp_config.json",
      project: "/repo/.agents/mcp_config.json",
      defaultScope: "user",
    },
    "antigravity-cli": {
      user: "/home/alice/.gemini/antigravity-cli/mcp_config.json",
      project: "/repo/.agents/mcp_config.json",
      defaultScope: "user",
    },
    gemini: { user: "/home/alice/.gemini/settings.json", project: "/repo/.gemini/settings.json", defaultScope: "user" },
    grok: { user: "/home/alice/.grok/config.toml", project: "/repo/.grok/config.toml", defaultScope: "user" },
  };
  for (const [client, targets] of Object.entries(expected)) {
    for (const scope of ["user", "project"]) {
      if (!targets[scope]) continue;
      assert.equal(resolveMCPConfigTarget(client, scope, roots).path, targets[scope]);
    }
    assert.equal(resolveMCPConfigTarget(client, undefined, roots).scope, targets.defaultScope);
  }
  assert.throws(() => resolveMCPConfigTarget("vscode", "user", roots), /Supported scopes: project/);
  assert.throws(() => resolveMCPConfigTarget("windsurf", "project", roots), /Supported scopes: user/);
  assert.throws(() => resolveMCPConfigTarget("custom", "user", roots), /does not have an automatic MCP config target/);
});

test("scope validation is explicit", () => {
  assert.equal(normalizeScope("USER"), "user");
  assert.throws(() => normalizeScope("local"), /Use user or project/);
});

test("print targets preserve provider format even when automatic user paths are unavailable", () => {
  const target = resolveMCPPrintTarget("vscode", "user");
  assert.equal(target.path, "");
  assert.equal(target.format, "json");
  assert.equal(target.rootKey, "servers");
});

test("client registry resolves native skill targets by scope", () => {
  const roots = { homeDir: "/home/alice", projectDir: "/repo" };
  const suffix = "/aipermission-operator/SKILL.md";
  const expected = {
    codex: { user: `/home/alice/.agents/skills${suffix}`, project: `/repo/.agents/skills${suffix}`, defaultScope: "user" },
    "claude-code": {
      user: `/home/alice/.claude/skills${suffix}`,
      project: `/repo/.claude/skills${suffix}`,
      defaultScope: "project",
    },
    cursor: { user: `/home/alice/.cursor/skills${suffix}`, project: `/repo/.cursor/skills${suffix}`, defaultScope: "project" },
    vscode: { user: `/home/alice/.copilot/skills${suffix}`, project: `/repo/.github/skills${suffix}`, defaultScope: "project" },
    copilot: { user: `/home/alice/.copilot/skills${suffix}`, project: `/repo/.github/skills${suffix}`, defaultScope: "user" },
    windsurf: {
      user: `/home/alice/.codeium/windsurf/skills${suffix}`,
      project: `/repo/.windsurf/skills${suffix}`,
      defaultScope: "user",
    },
    antigravity: {
      user: `/home/alice/.gemini/config/skills${suffix}`,
      project: `/repo/.agents/skills${suffix}`,
      defaultScope: "user",
    },
    "antigravity-cli": {
      user: `/home/alice/.gemini/antigravity-cli/skills${suffix}`,
      project: `/repo/.agents/skills${suffix}`,
      defaultScope: "user",
    },
    gemini: { user: `/home/alice/.gemini/skills${suffix}`, project: `/repo/.gemini/skills${suffix}`, defaultScope: "user" },
    grok: { user: `/home/alice/.grok/skills${suffix}`, project: `/repo/.grok/skills${suffix}`, defaultScope: "user" },
    agents: { user: `/home/alice/.agents/skills${suffix}`, project: `/repo/.agents/skills${suffix}`, defaultScope: "project" },
  };
  for (const [client, targets] of Object.entries(expected)) {
    assert.equal(resolveSkillTarget(client, "user", roots).path, targets.user);
    assert.equal(resolveSkillTarget(client, "project", roots).path, targets.project);
    assert.equal(resolveSkillTarget(client, undefined, roots).scope, targets.defaultScope);
  }
  assert.throws(() => resolveSkillTarget("custom", "user", roots), /does not have an automatic skill target/);
});

test("client registry applies client-specific server fields", () => {
  const base = { command: "npx", args: ["-y", "@aipermission/mcp@0.2.39"], env: {} };
  assert.deepEqual(adaptMCPServerConfig("copilot", base), { ...base, type: "local", tools: ["*"] });
  assert.deepEqual(adaptMCPServerConfig("grok", base), { ...base, startup_timeout_sec: 60 });
  assert.deepEqual(adaptMCPServerConfig("codex", base), base);
});

test("client registry honors documented client home overrides", () => {
  const roots = { projectDir: "/repo", env: { CODEX_HOME: "/alt/codex", COPILOT_HOME: "/alt/copilot", GROK_HOME: "/alt/grok" } };
  assert.equal(resolveMCPConfigTarget("codex", "user", roots).path, "/alt/codex/config.toml");
  assert.equal(resolveMCPConfigTarget("copilot", "user", roots).path, "/alt/copilot/mcp-config.json");
  assert.equal(resolveSkillTarget("copilot", "user", roots).path, "/alt/copilot/skills/aipermission-operator/SKILL.md");
  assert.equal(resolveMCPConfigTarget("grok", "user", roots).path, "/alt/grok/config.toml");
});
