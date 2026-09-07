import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { isBehaviorOwner, listBehaviorOwners } from "./coverage-owner-policy.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(frontendRoot, "..");
const baselinePath = join(frontendRoot, ".changed-coverage-baseline.json");
const summaryPath = join(frontendRoot, "coverage", "changed", "coverage-summary.json");
const updateBaseline = process.argv.includes("--update-baseline");
const floors = { statements: 75, branches: 60, functions: 70, lines: 75 };

const changedOwners = updateBaseline ? listBehaviorOwners(frontendRoot) : findChangedOwners();
if (!updateBaseline && changedOwners.length === 0) {
  console.log("Changed frontend coverage passed: no behavior owners changed.");
  process.exit(0);
}

runCoverage();
const coverage = readCoverageSummary();

if (updateBaseline) {
  const files = Object.fromEntries(changedOwners.map((file) => [file, metricsFor(file, coverage)]));
  writeFileSync(baselinePath, `${JSON.stringify({ version: 1, floors, files }, null, 2)}\n`);
  console.log(`Changed frontend coverage baseline updated for ${changedOwners.length} behavior owners.`);
  process.exit(0);
}

const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
const failures = [];
for (const file of changedOwners) {
  const actual = metricsFor(file, coverage);
  const required = baseline.files[file] ?? floors;
  for (const metric of Object.keys(floors)) {
    if (actual[metric] + 0.001 < required[metric]) {
      const source = baseline.files[file] ? "accepted baseline" : "new-file floor";
      failures.push(`${file} ${metric} ${actual[metric]}% is below ${source} ${required[metric]}%`);
    }
  }
}

if (failures.length > 0) {
  console.error("Changed frontend coverage failed:");
  failures.forEach((failure) => console.error(`- ${failure}`));
  process.exit(1);
}

console.log(`Changed frontend coverage passed for ${changedOwners.length} behavior owners.`);

function findChangedOwners() {
  const base = coverageBase();
  const output = execFileSync("git", ["diff", "--name-only", "--diff-filter=ACMR", `${base}...HEAD`, "--", "frontend/src"], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  return output
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((file) => file.replace(/^frontend\//, ""))
    .filter(isBehaviorOwner)
    .sort();
}

function coverageBase() {
  const requested = process.env.FRONTEND_COVERAGE_BASE;
  if (requested && !/^0+$/.test(requested)) return requested;
  for (const candidate of ["origin/main", "HEAD^"]) {
    const result = spawnSync("git", ["rev-parse", "--verify", candidate], { cwd: repositoryRoot, stdio: "ignore" });
    if (result.status === 0) return candidate;
  }
  throw new Error("Cannot determine a base commit for changed frontend coverage");
}

function runCoverage() {
  const vitest = join(frontendRoot, "node_modules", "vitest", "vitest.mjs");
  const result = spawnSync(process.execPath, [vitest, "run", "--config", "vitest.changed.config.js", "--coverage"], {
    cwd: frontendRoot,
    stdio: "inherit",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function readCoverageSummary() {
  if (!existsSync(summaryPath)) throw new Error(`Coverage summary was not created at ${summaryPath}`);
  return JSON.parse(readFileSync(summaryPath, "utf8"));
}

function metricsFor(file, coverage) {
  const absolute = resolve(frontendRoot, file);
  const entry = coverage[absolute] ?? Object.entries(coverage).find(([key]) => resolve(key) === absolute)?.[1];
  if (!entry) throw new Error(`Coverage did not report behavior owner ${file}`);
  return Object.fromEntries(Object.keys(floors).map((metric) => [metric, Number(entry[metric].pct)]));
}
