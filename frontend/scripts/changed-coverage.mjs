import { execFileSync, spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { isBehaviorOwner, listBehaviorOwners } from "./coverage-owner-policy.mjs";
import { coverageFloors as floors, ratchetedMetrics, validateCoverageBaseline } from "./coverage-ratchet.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(frontendRoot, "..");
const baselinePath = join(frontendRoot, ".changed-coverage-baseline.json");
const summaryPath = join(frontendRoot, "coverage", "changed", "coverage-summary.json");
const updateBaseline = process.argv.includes("--update-baseline");
const base = updateBaseline ? null : coverageBase();

const changedOwners = updateBaseline ? listBehaviorOwners(frontendRoot) : findChangedOwners();
if (!updateBaseline && changedOwners.length === 0 && !baselineChanged()) {
  console.log("Changed frontend coverage passed: no behavior owners changed.");
  process.exit(0);
}

runCoverage();
const coverage = readCoverageSummary();

if (updateBaseline) {
  if (process.env.CI) throw new Error("Refusing to update the changed coverage baseline in CI");
  const files = Object.fromEntries(changedOwners.map((file) => [file, metricsFor(file, coverage)]));
  writeFileSync(baselinePath, `${JSON.stringify({ version: 2, floors, files }, null, 2)}\n`);
  console.log(`Changed frontend coverage baseline updated for ${changedOwners.length} behavior owners.`);
  process.exit(0);
}

const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
const allOwners = listBehaviorOwners(frontendRoot);
validateCoverageBaseline(baseline, allOwners);
const baseBaseline = readBaseBaseline(base);
const failures = [];
for (const file of allOwners) {
  const actual = metricsFor(file, coverage);
  const accepted = baseline.files[file];
  const previous = baseBaseline?.files?.[file];
  for (const metric of Object.keys(floors)) {
    if (actual[metric] + 0.001 < accepted[metric]) {
      failures.push(`${file} ${metric} ${actual[metric]}% is below checked baseline ${accepted[metric]}%`);
    }
    if (previous && accepted[metric] + 0.001 < previous[metric]) {
      failures.push(`${file} ${metric} baseline ${accepted[metric]}% weakens base ${previous[metric]}%`);
    }
  }
}
for (const file of changedOwners) {
  const previous = baseBaseline?.files?.[file];
  const required = baseBaseline ? ratchetedMetrics(previous) : baseline.files[file];
  for (const metric of Object.keys(floors)) {
    if (baseline.files[file][metric] + 0.001 < required[metric]) {
      const source = previous ? "ratcheted base" : "new-file floor";
      failures.push(`${file} ${metric} baseline ${baseline.files[file][metric]}% is below ${source} ${required[metric]}%`);
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

function readBaseBaseline(ref) {
  const result = spawnSync("git", ["show", `${ref}:frontend/.changed-coverage-baseline.json`], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (result.status !== 0) return null;
  const baseline = JSON.parse(result.stdout);
  if (baseline?.version !== 2) throw new Error("Base changed coverage baseline must use version 2");
  return baseline;
}

function baselineChanged() {
  const result = spawnSync("git", ["diff", "--quiet", `${base}...HEAD`, "--", "frontend/.changed-coverage-baseline.json"], {
    cwd: repositoryRoot,
  });
  if (result.status === 0) return false;
  if (result.status === 1) return true;
  throw result.error || new Error("Cannot compare the changed coverage baseline with its base revision");
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
