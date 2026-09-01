import assert from "node:assert/strict";
import test from "node:test";

import { clientLabel, getClient, MCP_PROVIDERS, normalizeClientID } from "../src/client-registry.js";

test("client registry exposes MCP setup providers", () => {
  assert.deepEqual(
    MCP_PROVIDERS.map((provider) => provider.id),
    ["codex", "claude-code", "cursor", "vscode", "windsurf", "antigravity", "gemini", "custom"],
  );
  assert.equal(getClient("OpenAI Codex").id, "codex");
});

test("client registry normalizes aliases from one source", () => {
  assert.equal(normalizeClientID("claude"), "claude-code");
  assert.equal(normalizeClientID("copilot"), "vscode");
  assert.equal(normalizeClientID("agy"), "antigravity");
  assert.equal(clientLabel("vscode"), "VS Code / GitHub Copilot");
  assert.throws(() => normalizeClientID("unknown"), /Unknown client/);
});
