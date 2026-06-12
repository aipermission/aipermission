#!/usr/bin/env node

const command = process.argv[2] || "serve";

if (command === "init") {
  const { runInit } = await import("./init.js");
  await runInit(process.argv.slice(3));
} else if (command === "install-skill") {
  const { runInstallSkill } = await import("./install-skill.js");
  await runInstallSkill(process.argv.slice(3));
} else if (command === "serve" || command === "server" || command === "start") {
  await import("./server.js");
} else if (command === "--help" || command === "-h" || command === "help") {
  printHelp();
} else {
  console.error(`Unknown command: ${command}`);
  printHelp();
  process.exit(1);
}

function printHelp() {
  console.log(`aipermission MCP

Usage:
  aipermission-mcp                 Start the MCP stdio server
  aipermission-mcp init            Configure an AI client interactively
  aipermission-mcp install-skill   Install the operator skill for an AI client

Init flags:
  --provider codex|claude-code|cursor|vscode|windsurf|antigravity|gemini|custom
  --name aipermission
  --token-stdin
  --api-url http://localhost:3210
  --print

Install skill flags:
  --client codex|claude-code|cursor|vscode|windsurf|antigravity|gemini|custom
  --project-dir /path/to/workspace
  --source /path/to/SKILL.md  Local file only; HTTP(S) sources are rejected

Security:
  Use the hidden token prompt or --token-stdin. AIPERMISSION_API_URL must point
  to localhost, 127.0.0.1, or [::1].
	`);
}
