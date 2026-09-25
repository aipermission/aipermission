import { mcpPackageName } from "./mcp-package.ts";

export function buildMCPSetupCommand({
  provider,
  name,
  apiUrl,
  print = false,
}: {
  provider: string;
  name: string;
  apiUrl: string;
  print?: boolean;
}): string {
  const printFlag = print ? " \\\n  --print" : "";
  return `npx -y ${mcpPackageName} setup \\
  --provider ${provider} \\
  --name ${name} \\
  --api-url ${shellQuote(apiUrl)}${printFlag}`;
}

function shellQuote(value: string): string {
  return `'${String(value).replaceAll("'", `'"'"'`)}'`;
}
