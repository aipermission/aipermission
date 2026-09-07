import { readdirSync } from "node:fs";
import { basename, extname, join, relative, sep } from "node:path";

const excludedNames = new Set(["mcp-client-catalog.js", "release.generated.json"]);
const excludedDirectories = new Set(["src/test"]);
const nodeCoverageDirectories = new Set(["src/lib/local-action-retry"]);

export function isBehaviorOwner(file) {
  const normalized = file.split(sep).join("/");
  if (!normalized.startsWith("src/") || ![".js", ".jsx"].includes(extname(normalized))) return false;
  if (normalized.includes(".test.") || excludedNames.has(basename(normalized))) return false;
  if (/^src\/connectors\/templates\/[^/]+\/index\.jsx$/.test(normalized)) return false;
  if ([...excludedDirectories].some((directory) => normalized === directory || normalized.startsWith(`${directory}/`))) return false;
  if ([...nodeCoverageDirectories].some((directory) => normalized.startsWith(`${directory}/`))) return false;
  return true;
}

export function listBehaviorOwners(root) {
  return sourceFiles(join(root, "src"))
    .map((file) => relative(root, file).split(sep).join("/"))
    .filter(isBehaviorOwner)
    .sort();
}

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(path);
    return [path];
  });
}
