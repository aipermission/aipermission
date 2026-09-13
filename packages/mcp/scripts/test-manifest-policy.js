import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import trustedGitBase from "../../../scripts/trusted-git-base.js";

const { resolveTrustedBase } = trustedGitBase;
const packageRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = path.resolve(packageRoot, "../..");
const manifestPath = path.join(packageRoot, "test/test-manifest.json");
const privateFileTestPath = path.join(packageRoot, "test/private-file.test.js");

export const requiredWindowsACLTest = "atomicWritePrivateFile applies a protected Windows ACL";
export const requiredMinimumTests = 116;
export const permittedMaximumSkipped = 1;

export function readTestManifest(source, label = "MCP test manifest") {
  let manifest;
  try {
    manifest = JSON.parse(source);
  } catch (error) {
    throw new Error(`${label} is invalid JSON: ${error.message}`, { cause: error });
  }
  for (const key of ["files", "windowsACLTests"]) {
    if (!Array.isArray(manifest[key]) || manifest[key].length === 0 || new Set(manifest[key]).size !== manifest[key].length) {
      throw new Error(`${label} must define unique, non-empty ${key}`);
    }
    if (manifest[key].some((value) => typeof value !== "string" || !value.trim())) {
      throw new Error(`${label} has an invalid ${key} entry`);
    }
  }
  if (!Number.isInteger(manifest.minimumTests) || manifest.minimumTests < requiredMinimumTests) {
    throw new Error(`${label} minimumTests must be at least ${requiredMinimumTests}`);
  }
  if (!Number.isInteger(manifest.maximumSkipped) || manifest.maximumSkipped < 0 || manifest.maximumSkipped > permittedMaximumSkipped) {
    throw new Error(`${label} maximumSkipped must be between 0 and ${permittedMaximumSkipped}`);
  }
  if (!manifest.windowsACLTests.includes(requiredWindowsACLTest)) {
    throw new Error(`${label} is missing required Windows ACL integration test: ${requiredWindowsACLTest}`);
  }
  return manifest;
}

export function verifyTestManifestRatchet(previous, current) {
  if (!previous) return;
  const failures = [];
  for (const key of ["files", "windowsACLTests"]) {
    const currentValues = new Set(current[key]);
    for (const value of previous[key]) {
      if (!currentValues.has(value)) failures.push(`${key} removed required entry ${value}`);
    }
  }
  if (current.minimumTests < previous.minimumTests) {
    failures.push(`minimumTests decreased from ${previous.minimumTests} to ${current.minimumTests}`);
  }
  if (current.maximumSkipped > previous.maximumSkipped) {
    failures.push(`maximumSkipped increased from ${previous.maximumSkipped} to ${current.maximumSkipped}`);
  }
  if (failures.length > 0) throw new Error(`MCP test manifest weakened:\n- ${failures.join("\n- ")}`);
}

export function verifyRequiredWindowsACLSource(source) {
  const declaration = `test("${requiredWindowsACLTest}", { skip: process.platform !== "win32" }`;
  if (!source.includes(declaration)) {
    throw new Error(`Missing fail-closed Windows ACL integration test declaration: ${requiredWindowsACLTest}`);
  }
}

export function loadAndVerifyTestManifest() {
  const current = readTestManifest(fs.readFileSync(manifestPath, "utf8"));
  verifyRequiredWindowsACLSource(fs.readFileSync(privateFileTestPath, "utf8"));
  const base = resolveTrustedBase({
    configured: process.env.MCP_TEST_MANIFEST_BASE,
    variable: "MCP_TEST_MANIFEST_BASE",
    root: repositoryRoot,
  });
  const previousSource = gitFileAt(base, "packages/mcp/test/test-manifest.json");
  const previous = previousSource ? readTestManifest(previousSource, `MCP test manifest at ${base}`) : null;
  verifyTestManifestRatchet(previous, current);
  return current;
}

function gitFileAt(ref, file) {
  const listing = spawnSync("git", ["ls-tree", "--name-only", ref, "--", file], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (listing.error) throw listing.error;
  if (listing.status !== 0) throw new Error(`Cannot inspect MCP test manifest at ${ref}: ${listing.stderr.trim()}`);
  if (!listing.stdout.trim()) return "";
  const result = spawnSync("git", ["show", `${ref}:${file}`], { cwd: repositoryRoot, encoding: "utf8" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Cannot read MCP test manifest at ${ref}: ${result.stderr.trim()}`);
  return result.stdout;
}
