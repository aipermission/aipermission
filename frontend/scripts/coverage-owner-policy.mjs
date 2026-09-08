import { readFileSync, readdirSync } from "node:fs";
import { basename, extname, join, relative, sep } from "node:path";
import { coveragePolicy } from "./coverage-policy.mjs";

const excludedNames = new Set(coveragePolicy.excludedNames);
const excludedDirectories = new Set(coveragePolicy.excludedDirectories);
const separatelyCoveredDirectories = new Set(coveragePolicy.separatelyCoveredDirectories);
const excludedPatterns = coveragePolicy.excludedPatterns.map((pattern) => new RegExp(pattern));
const architecturePolicy = JSON.parse(readFileSync(new URL("../architecture-policy.json", import.meta.url), "utf8"));
const sourceExtensions = new Set(architecturePolicy.sourceExtensions);

export function isBehaviorOwner(file) {
  const normalized = file.split(sep).join("/");
  if (!normalized.startsWith("src/") || !sourceExtensions.has(extname(normalized))) return false;
  if (normalized.includes(".test.") || excludedNames.has(basename(normalized))) return false;
  if (excludedPatterns.some((pattern) => pattern.test(normalized))) return false;
  if ([...excludedDirectories].some((directory) => normalized === directory || normalized.startsWith(`${directory}/`))) return false;
  if ([...separatelyCoveredDirectories].some((directory) => normalized.startsWith(`${directory}/`))) return false;
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
