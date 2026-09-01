#!/usr/bin/env node

import { getClientCatalog } from "./client-registry.js";

const command = process.argv[2] || "serve";

if (command === "init" || command === "setup") {
  const { runInit, runSetup } = await import("./init.js");
  await (command === "setup" ? runSetup : runInit)(process.argv.slice(3));
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
  const catalog = getClientCatalog();
  const mcpClients = catalog.filter((client) => client.supportsMCP).map((client) => client.id);
  const skillClients = catalog.filter((client) => client.supportsSkill).map((client) => client.id);
  const doctorClients = catalog.filter((client) => client.supportsMCP && client.supportsSkill).map((client) => client.id);
  console.log(`aipermission MCP

Usage:
  aipermission-mcp                 Start the MCP stdio server
  aipermission-mcp init            Configure an AI client interactively
  aipermission-mcp setup           Configure MCP and install its native skill
  aipermission-mcp install-skill   Install the operator skill for an AI client
  aipermission-mcp doctor          Check MCP config and native skill paths

Init flags:
  --provider ${[...mcpClients, "custom"].join("|")}
  --scope user|project
  --name aipermission
  --token-stdin
  --api-url http://localhost:3210
  --print                         Print a token-placeholder preview; change no files
  --force
  --home /path/to/home
  --project-dir /path/to/workspace

Setup-only flags:
  --skill-scope user|project
  --skill-source /path/to/SKILL.md  Local file only

Install skill flags:
  --client ${[...skillClients, "custom"].join("|")}
  --scope user|project
  --home /path/to/home
  --project-dir /path/to/workspace
  --source /path/to/SKILL.md  Local file only; HTTP(S) sources are rejected

Doctor flags:
  --client ${doctorClients.join("|")}
  --scope user|project
  --mcp-scope user|project
  --skill-scope user|project
  --name aipermission
  --home /path/to/home
  --project-dir /path/to/workspace

Security:
  Use the hidden token prompt or --token-stdin. AIPERMISSION_API_URL must point
  to localhost, 127.0.0.1, or [::1].
	`);
}
