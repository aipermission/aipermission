import assert from "node:assert/strict";
import test from "node:test";

import { buildMCPSetupCommand } from "./mcp-setup-command.js";

test("MCP setup commands preserve the configured gateway origin", () => {
  const command = buildMCPSetupCommand({
    provider: "codex",
    name: "aipermission-default",
    apiUrl: "http://localhost:3211",
  });

  assert.match(command, /--api-url 'http:\/\/localhost:3211'/);
  assert.doesNotMatch(command, /localhost:3210/);
});

test("MCP setup commands safely quote IPv6 gateway origins", () => {
  const command = buildMCPSetupCommand({
    provider: "custom",
    name: "aipermission-default",
    apiUrl: "http://[::1]:3212",
    print: true,
  });

  assert.match(command, /--api-url 'http:\/\/\[::1\]:3212'/);
  assert.doesNotMatch(command, /\n\+/);
  assert.match(command, /--print$/);
});

test("MCP setup commands quote unexpected shell metacharacters", () => {
  const command = buildMCPSetupCommand({
    provider: "custom",
    name: "aipermission-default",
    apiUrl: "http://localhost:3210/'quoted'",
  });

  assert.match(command, /--api-url 'http:\/\/localhost:3210\/'"'"'quoted'"'"''/);
});
