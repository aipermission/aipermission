import { spawnSync } from "node:child_process";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { isBehaviorOwner, listBehaviorOwners } from "./coverage-owner-policy.mjs";
import { coveragePolicy, coveragePolicyWeakening, legacyCoveragePolicy, readCoveragePolicy } from "./coverage-policy.mjs";
import { findChangedOwnerEntries, readBaselineAt, resolveBootstrapRevision } from "./coverage-git-state.mjs";
import { createCoverageReportDirectory } from "./coverage-report-directory.mjs";
import {
  coverageFloors as floors,
  mergeChangedCoverageBaseline,
  requiredChangedMetrics,
  validateCoverageBaseline,
  validateCoverageBaselineForRun,
} from "./coverage-ratchet.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = resolve(frontendRoot, "..");
const baselinePath = join(frontendRoot, ".changed-coverage-baseline.json");
const updateBaseline = process.argv.includes("--update-baseline");
const base = coverageBase();
const allOwners = listBehaviorOwners(frontendRoot);
const baseline = JSON.parse(readFileSync(baselinePath, "utf8"));
validateCoverageBaselineForRun(baseline, allOwners, updateBaseline);
const comparison = coverageComparison(base, baseline);
const policyFailures = coveragePolicyWeakening(readPolicyAt(comparison.ref), coveragePolicy);
if (policyFailures.length > 0) throw new Error(`Changed coverage policy weakened:\n- ${policyFailures.join("\n- ")}`);
const changedEntries = findChangedOwnerEntries(repositoryRoot, comparison.ref, isBehaviorOwner);
const changedOwners = changedEntries.map((entry) => entry.file);
const changedStatus = new Map(changedEntries.map((entry) => [entry.file, entry.status]));
if (!updateBaseline && changedOwners.length === 0 && !baselineChanged()) {
  console.log("Changed frontend coverage passed: no behavior owners changed.");
  process.exit(0);
}

const reportDirectory = createCoverageReportDirectory();
process.once("exit", reportDirectory.cleanup);
runCoverage(reportDirectory.path);
const coverage = readCoverageSummary(reportDirectory.path);

if (updateBaseline) {
  if (process.env.CI) throw new Error("Refusing to update the changed coverage baseline in CI");
  const currentBaseline = existsSync(baselinePath) ? JSON.parse(readFileSync(baselinePath, "utf8")) : null;
  const measuredFiles = Object.fromEntries(allOwners.map((file) => [file, metricsFor(file, coverage)]));
  const requiredFiles = Object.fromEntries(
    changedOwners.map((file) => [
      file,
      requiredChangedMetrics({
        baseBaselineAvailable: Boolean(comparison.baseline),
        previous: comparison.baseline?.files?.[file],
        added: changedStatus.get(file) === "A" || changedStatus.get(file) === "C",
        accepted: currentBaseline?.files?.[file] || measuredFiles[file],
      }),
    ]),
  );
  const files = mergeChangedCoverageBaseline(allOwners, changedOwners, currentBaseline?.files, measuredFiles, requiredFiles);
  writeFileSync(
    baselinePath,
    `${JSON.stringify(
      { version: 2, bootstrap_revision: baseline.bootstrap_revision, bootstrap_tree: baseline.bootstrap_tree, floors, files },
      null,
      2,
    )}\n`,
  );
  console.log(`Changed frontend coverage baseline updated for ${changedOwners.length} behavior owners.`);
  process.exit(0);
}

const baseBaseline = comparison.baseline;
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
  const required = requiredChangedMetrics({
    baseBaselineAvailable: Boolean(baseBaseline),
    previous,
    added: changedStatus.get(file) === "A" || changedStatus.get(file) === "C",
    accepted: baseline.files[file],
  });
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

function readBaseBaseline(ref) {
  const baseline = readBaselineAt(repositoryRoot, ref);
  if (baseline === null) return null;
  validateCoverageBaseline(baseline);
  return baseline;
}

function coverageComparison(ref, checkedBaseline) {
  const baseline = readBaseBaseline(ref);
  if (baseline) return { ref, baseline };
  const bootstrap = checkedBaseline.bootstrap_revision;
  const resolvedBootstrap = resolveBootstrapRevision(repositoryRoot, {
    revision: bootstrap,
    tree: checkedBaseline.bootstrap_tree,
  });
  assertAncestor(ref, resolvedBootstrap, "coverage base must be an ancestor of the accepted bootstrap tree");
  const bootstrapBaseline = readBaseBaseline(resolvedBootstrap);
  if (!bootstrapBaseline) throw new Error(`Coverage bootstrap revision ${resolvedBootstrap} does not contain a baseline`);
  return { ref: resolvedBootstrap, baseline: bootstrapBaseline };
}

function assertAncestor(ancestor, descendant, message) {
  const result = spawnSync("git", ["merge-base", "--is-ancestor", ancestor, descendant], { cwd: repositoryRoot });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(message);
}

function baselineChanged() {
  const result = spawnSync("git", ["diff", "--quiet", comparison.ref, "--", "frontend/.changed-coverage-baseline.json"], {
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

function readPolicyAt(ref) {
  const result = spawnSync("git", ["show", `${ref}:frontend/coverage-policy.json`], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (result.status === 0) return readCoveragePolicy(result.stdout);
  return legacyCoveragePolicy;
}

function runCoverage(reportPath) {
  const vitest = join(frontendRoot, "node_modules", "vitest", "vitest.mjs");
  const result = spawnSync(
    process.execPath,
    [vitest, "run", "--config", "vitest.changed.config.js", "--coverage", `--coverage.reportsDirectory=${reportPath}`],
    {
      cwd: frontendRoot,
      stdio: "inherit",
    },
  );
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function readCoverageSummary(reportPath) {
  const summaryPath = join(reportPath, "coverage-summary.json");
  if (!existsSync(summaryPath)) throw new Error(`Coverage summary was not created at ${summaryPath}`);
  return JSON.parse(readFileSync(summaryPath, "utf8"));
}

function metricsFor(file, coverage) {
  const absolute = resolve(frontendRoot, file);
  const entry = coverage[absolute] ?? Object.entries(coverage).find(([key]) => resolve(key) === absolute)?.[1];
  if (!entry) throw new Error(`Coverage did not report behavior owner ${file}`);
  return Object.fromEntries(Object.keys(floors).map((metric) => [metric, Number(entry[metric].pct)]));
}
