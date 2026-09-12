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
export const connectorTemplateKinds = readdirSync(connectorTemplatesDir, { withFileTypes: true })
  .filter((entry) => entry.isDirectory() && !entry.name.startsWith("_"))
  .map((entry) => entry.name)
  .sort();
