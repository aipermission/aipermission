#!/usr/bin/env node

const command = process.argv[2] || "serve";

if (command === "init" || command === "setup") {
  const { runInit } = await import("./init.js");
  const args = command === "setup" ? ["--install-skill", ...process.argv.slice(3)] : process.argv.slice(3);
  await runInit(args);
} else if (command === "install-skill") {
  const { runInstallSkill } = await import("./install-skill.js");
  await runInstallSkill(process.argv.slice(3));
} else if (command === "doctor") {
  const { runDoctor } = await import("./doctor.js");
  const result = await runDoctor(process.argv.slice(3));
  if (!result.ok) process.exitCode = 1;
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
  aipermission-mcp setup           Configure MCP and install its native skill
  aipermission-mcp install-skill   Install the operator skill for an AI client
  aipermission-mcp doctor          Check MCP config and native skill paths

Init flags:
  --provider codex|claude-code|cursor|vscode|copilot|windsurf|antigravity|gemini|grok|custom
  --scope user|project
  --name aipermission
  --token-stdin
  --api-url http://localhost:3210
  --print
  --install-skill
  --skill-source /path/to/SKILL.md  Local file only

Install skill flags:
  --client codex|claude-code|cursor|vscode|copilot|windsurf|antigravity|gemini|grok|agents|custom
  --scope user|project
  --project-dir /path/to/workspace
  --source /path/to/SKILL.md  Local file only; HTTP(S) sources are rejected

Doctor flags:
  --client codex|claude-code|cursor|vscode|copilot|windsurf|antigravity|gemini|grok
  --scope user|project
  --name aipermission
  --project-dir /path/to/workspace

Security:
  Use the hidden token prompt or --token-stdin. AIPERMISSION_API_URL must point
  to localhost, 127.0.0.1, or [::1].
	`);
}
