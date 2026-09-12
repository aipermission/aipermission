#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { getClientCatalog } from "../packages/mcp/src/client-registry.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const outputPath = path.join(root, "frontend/src/lib/mcp-client-catalog.js");

function renderCatalog(catalog) {
  const clients = catalog.map(({ id, label, supportsMCP, supportsSkill }) => ({
    id,
    label,
    supportsMCP,
    supportsSkill,
  }));
  const entries = clients
    .map(
      (client) => `  {
    id: ${JSON.stringify(client.id)},
    label: ${JSON.stringify(client.label)},
    supportsMCP: ${client.supportsMCP},
    supportsSkill: ${client.supportsSkill},
  },`,
    )
    .join("\n");
  return `// Generated from packages/mcp/src/client-registry.js. Do not edit directly.\n\nexport const mcpClientCatalog = Object.freeze([\n${entries}\n]);\n`;
}

const expected = renderCatalog(getClientCatalog());
if (process.argv[2] === "--check") {
  const current = fs.existsSync(outputPath)
    ? fs.readFileSync(outputPath, "utf8")
    : "";
  if (current !== expected) {
    throw new Error(
      "frontend MCP client catalog is stale; run npm run mcp-client-catalog",
    );
  }
  console.log("Generated MCP client catalog is current.");
} else {
  fs.writeFileSync(outputPath, expected);
  console.log(`Updated ${path.relative(root, outputPath)}.`);
}
