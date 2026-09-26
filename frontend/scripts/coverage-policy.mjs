import { readFileSync } from "node:fs";

export const legacyCoveragePolicy = Object.freeze({
  floors: { statements: 75, branches: 60, functions: 70, lines: 75 },
  debtStep: 1,
  excludedNames: ["mcp-client-catalog.js", "release.generated.json"],
  excludedDirectories: ["src/test"],
  separatelyCoveredDirectories: ["src/lib/local-action-retry"],
  excludedPatterns: ["^src/connectors/templates/[^/]+/index\\.jsx$"],
});

export const coveragePolicy = readCoveragePolicy(readFileSync(new URL("../coverage-policy.json", import.meta.url), "utf8"));

export function readCoveragePolicy(source) {
  const policy = typeof source === "string" ? JSON.parse(source) : source;
  for (const metric of ["statements", "branches", "functions", "lines"]) {
    const value = policy?.floors?.[metric];
    if (!Number.isFinite(value) || value < 0 || value > 100) throw new Error(`Invalid coverage floor for ${metric}`);
  }
  if (!Number.isFinite(policy?.debtStep) || policy.debtStep <= 0) throw new Error("Coverage debtStep must be positive");
  for (const name of ["excludedNames", "excludedDirectories", "separatelyCoveredDirectories", "excludedPatterns"]) {
    if (!Array.isArray(policy?.[name]) || policy[name].some((value) => typeof value !== "string" || !value)) {
      throw new Error(`Coverage policy ${name} must contain non-empty strings`);
    }
  }
  policy.excludedPatterns.forEach((pattern) => new RegExp(pattern));
  return policy;
}

export function coveragePolicyWeakening(base, current) {
  const failures = [];
  const oldManifestPattern = "^src/connectors/templates/[^/]+/index\\.jsx$";
  const typedManifestPattern = "^src/connectors/templates/[^/]+/index\\.ts$";
  for (const [metric, value] of Object.entries(current.floors)) {
    if (value < base.floors[metric]) failures.push(`${metric} floor decreased from ${base.floors[metric]} to ${value}`);
  }
  if (current.debtStep < base.debtStep) failures.push(`debtStep decreased from ${base.debtStep} to ${current.debtStep}`);
  for (const name of ["excludedNames", "excludedDirectories", "separatelyCoveredDirectories", "excludedPatterns"]) {
    const accepted = new Set(base[name]);
    for (const value of current[name]) {
      const migratedManifest =
        name === "excludedPatterns" &&
        value === typedManifestPattern &&
        accepted.has(oldManifestPattern) &&
        !current.excludedPatterns.includes(oldManifestPattern);
      const migratedCatalog =
        name === "excludedNames" &&
        value === "mcp-client-catalog.ts" &&
        accepted.has("mcp-client-catalog.js") &&
        !current.excludedNames.includes("mcp-client-catalog.js");
      if (!accepted.has(value) && !migratedManifest && !migratedCatalog) failures.push(`${name} added unreviewed exclusion ${value}`);
    }
  }
  return failures;
}
