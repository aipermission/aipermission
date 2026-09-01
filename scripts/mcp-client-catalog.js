#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { pathToFileURL } = require("node:url");

const root = path.resolve(__dirname, "..");
const registryPath = path.join(root, "packages/mcp/src/client-registry.js");
const outputPath = path.join(root, "frontend/src/lib/mcp-client-catalog.js");

function renderCatalog(catalog) {
  const clients = catalog.map(({ id, label, supportsMCP, supportsSkill }) => ({ id, label, supportsMCP, supportsSkill }));
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

async function expectedCatalog() {
  const registry = await import(`${pathToFileURL(registryPath).href}?catalog=${Date.now()}`);
  return renderCatalog(registry.getClientCatalog());
}

async function main() {
  const expected = await expectedCatalog();
  if (process.argv[2] === "--check") {
    const current = fs.existsSync(outputPath) ? fs.readFileSync(outputPath, "utf8") : "";
    if (current !== expected) throw new Error("frontend MCP client catalog is stale; run npm run mcp-client-catalog");
    console.log("Generated MCP client catalog is current.");
    return;
  }
  fs.writeFileSync(outputPath, expected);
  console.log(`Updated ${path.relative(root, outputPath)}.`);
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
});
