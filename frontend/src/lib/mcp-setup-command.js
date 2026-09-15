import { mcpPackageName } from "./mcp-package.js";

export function buildMCPSetupCommand({ provider, name, apiUrl, print = false }) {
  const printFlag = print ? " \\\n  --print" : "";
  return `npx -y ${mcpPackageName} setup \\
  --provider ${provider} \\
  --name ${name} \\
  --api-url ${shellQuote(apiUrl)}${printFlag}`;
}

function shellQuote(value) {
  return `'${String(value).replaceAll("'", `'"'"'`)}'`;
}
