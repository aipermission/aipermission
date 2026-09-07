import { readdirSync } from "node:fs";
import { basename, extname, join, relative, sep } from "node:path";

const excludedNames = new Set(["mcp-client-catalog.js", "release.generated.json"]);

export function isBehaviorOwner(file) {
  const normalized = file.split(sep).join("/");
  if (!normalized.startsWith("src/") || ![".js", ".jsx"].includes(extname(normalized))) return false;
  if (normalized.includes(".test.") || excludedNames.has(basename(normalized))) return false;

  const name = basename(normalized);
  return (
    /^src\/pages\/[^/]+\.(?:js|jsx)$/.test(normalized) ||
    /^use-/.test(name) ||
    /(?:approval|permission|reconciliation|reducer|session|transfer)/.test(name) ||
    /src\/connectors\/templates\/[^/]+\/console\.jsx$/.test(normalized)
  );
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
