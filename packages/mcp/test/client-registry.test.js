import assert from "node:assert/strict";
import test from "node:test";

import {
  clientLabel,
  getClient,
  MCP_PROVIDERS,
  normalizeClientID,
  normalizeScope,
  resolveMCPConfigTarget,
} from "../src/client-registry.js";

test("client registry exposes MCP setup providers", () => {
  assert.deepEqual(
    MCP_PROVIDERS.map((provider) => provider.id),
    ["codex", "claude-code", "cursor", "vscode", "copilot", "windsurf", "antigravity", "gemini", "grok", "custom"],
  );
  assert.equal(getClient("OpenAI Codex").id, "codex");
});

test("client registry normalizes aliases from one source", () => {
  assert.equal(normalizeClientID("claude"), "claude-code");
  assert.equal(normalizeClientID("copilot"), "copilot");
  assert.equal(normalizeClientID("agy"), "antigravity");
  assert.equal(clientLabel("vscode"), "VS Code");
  assert.throws(() => normalizeClientID("unknown"), /Unknown client/);
});

test("client registry resolves verified MCP targets by scope", () => {
  const roots = { homeDir: "/home/alice", projectDir: "/repo" };
  assert.equal(resolveMCPConfigTarget("codex", "project", roots).path, "/repo/.codex/config.toml");
  assert.equal(resolveMCPConfigTarget("copilot", "user", roots).path, "/home/alice/.copilot/mcp-config.json");
  assert.equal(resolveMCPConfigTarget("antigravity", "project", roots).path, "/repo/.agents/mcp_config.json");
  assert.equal(resolveMCPConfigTarget("grok", "user", roots).path, "/home/alice/.grok/config.toml");
  assert.equal(resolveMCPConfigTarget("claude", undefined, roots).scope, "project");
  assert.throws(() => resolveMCPConfigTarget("vscode", "user", roots), /Supported scopes: project/);
  assert.throws(() => resolveMCPConfigTarget("custom", "user", roots), /does not have an automatic MCP config target/);
});

test("scope validation is explicit", () => {
  assert.equal(normalizeScope("USER"), "user");
  assert.throws(() => normalizeScope("local"), /Use user or project/);
});
