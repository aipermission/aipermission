import { readdirSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export const currentDir = join(dirname(fileURLToPath(import.meta.url)), "..");
export const sourceDir = join(currentDir, "..");
export const connectorTemplatesDir = join(sourceDir, "connectors", "templates");
export const indexSource = readFileSync(join(sourceDir, "..", "index.html"), "utf8");
export const themeInitSource = readFileSync(join(sourceDir, "..", "public", "theme-init.js"), "utf8");
export const appSource = readFileSync(join(sourceDir, "App.jsx"), "utf8");
export const apiSource = readFileSync(join(currentDir, "api.js"), "utf8");
export const nginxSource = readFileSync(join(sourceDir, "..", "nginx.conf"), "utf8");
export const sidebarSource = readFileSync(join(sourceDir, "components", "app-sidebar.jsx"), "utf8");
export const releaseSource = readFileSync(join(currentDir, "release.js"), "utf8");
export const releaseData = JSON.parse(readFileSync(join(currentDir, "release.generated.json"), "utf8"));
export const releaseManifest = JSON.parse(readFileSync(join(sourceDir, "..", "..", "release-manifest.json"), "utf8"));
export const connectorTemplateRegistrySource = readFileSync(join(connectorTemplatesDir, "registry.jsx"), "utf8");
export const connectorTemplateCatalogSource = readFileSync(join(connectorTemplatesDir, "catalog.js"), "utf8");
export const backendConnectorRegistrySource = readFileSync(
  join(sourceDir, "..", "..", "backend", "internal", "connectors", "builtin", "registry.go"),
  "utf8",
);
export const connectorTemplateKinds = readdirSync(connectorTemplatesDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && !entry.name.startsWith("_"))
  .map((entry) => entry.name)
  .sort();

export function backendRegisteredConnectorKinds(source) {
  const connectorImports = new Map();
  for (const match of source.matchAll(/(\w+)\s+"github\.com\/aipermission\/aipermission\/backend\/internal\/connectors\/([^"]+)"/g)) {
    connectorImports.set(match[1], match[2].split("/")[0]);
  }
  return [...new Set([...source.matchAll(/(\w+)\.New\(\)/g)].map((match) => connectorImports.get(match[1])).filter(Boolean))].sort();
}
