#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const packages = ["frontend", "packages/mcp"];

function readJSON(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(root, relativePath), "utf8"));
}

function canonicalize(value) {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === "object") {
    return Object.fromEntries(
      Object.keys(value)
        .sort()
        .map((key) => [key, canonicalize(value[key])]),
    );
  }
  return value;
}

function stable(value) {
  return JSON.stringify(canonicalize(value ?? {}));
}

const failures = [];
const rootManifest = readJSON("package.json");
if (fs.existsSync(path.join(root, "package-lock.json"))) {
  failures.push("root package-lock.json must not exist; package-local lockfiles are canonical");
}
if (
  (Array.isArray(rootManifest.workspaces) && rootManifest.workspaces.length > 0) ||
  (rootManifest.workspaces && !Array.isArray(rootManifest.workspaces) && Object.keys(rootManifest.workspaces).length > 0)
) {
  failures.push("root package.json must not define workspaces; package-local lockfiles are canonical");
}

for (const directory of packages) {
  const manifest = readJSON(`${directory}/package.json`);
  const lock = readJSON(`${directory}/package-lock.json`);
  if (lock.lockfileVersion !== 3) {
    failures.push(`${directory}/package-lock.json must use lockfileVersion 3`);
  }
  const lockedPackage = lock.packages?.[""];
  if (!lockedPackage) {
    failures.push(`${directory}/package-lock.json is missing packages[\"\"] metadata`);
    continue;
  }
  for (const field of ["name", "version", "dependencies", "devDependencies", "engines", "bin"]) {
    if (stable(manifest[field]) !== stable(lockedPackage[field])) {
      failures.push(`${directory}: package.json ${field} does not match package-lock.json`);
    }
  }
}

if (failures.length > 0) {
  console.error(`Package lock ownership check failed:\n${failures.map((failure) => `- ${failure}`).join("\n")}`);
  process.exit(1);
}

console.log("Package-local lockfiles are canonical and match their manifests.");
