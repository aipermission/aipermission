#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const policy = require("../maintenance-policy.json");
const { isTestSource } = require("./maintenance-source-kind");
const failures = [];

function sourceLineCount(file) {
  const source = fs.readFileSync(file, "utf8");
  if (source.length === 0) return 0;
  const lines = source.split("\n").length;
  return source.endsWith("\n") ? lines - 1 : lines;
}

function walk(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    if (entry.isDirectory() && entry.name === "node_modules") return [];
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(entryPath) : [entryPath];
  });
}

function matchingSources(budget) {
  const extensions = new Set(budget.extensions);
  return walk(path.join(root, budget.directory)).filter((file) =>
    extensions.has(path.extname(file)),
  );
}

function testSource(budget, file) {
  return isTestSource(
    budget.classifier,
    file,
    policy.frontendArchitecture.testModuleMarkers,
  );
}

function positiveInteger(value) {
  return Number.isInteger(value) && value > 0;
}

function isAPIExceptionPath(value) {
  return /(^|\/)internal\/api(?:\/|$)/.test(value);
}

function validatePolicy(candidate = policy, target = failures) {
  if (candidate.version !== 1)
    target.push("maintenance policy version must be 1");
  const allowedMarkers = [".spec.", ".test."];
  const markers = [
    ...(candidate.frontendArchitecture?.testModuleMarkers || []),
  ].sort();
  if (JSON.stringify(markers) !== JSON.stringify(allowedMarkers)) {
    target.push(
      "frontend test module markers must be exactly .test. and .spec.",
    );
  }
  const identifiers = new Set();
  for (const budget of candidate.sourceBudgets || []) {
    if (identifiers.has(budget.id)) {
      target.push(`duplicate source budget id ${budget.id}`);
    }
    identifiers.add(budget.id);
    if (!budget.directory || !budget.extensions?.length) {
      target.push(`source budget ${budget.id} is incomplete`);
    }
    for (const name of [
      "productionMaxLines",
      "testMaxLines",
      "testPackageMaxLines",
    ]) {
      if (budget[name] !== undefined && !positiveInteger(budget[name])) {
        target.push(
          `source budget ${budget.id} ${name} must be a positive integer`,
        );
      }
    }
    if (
      budget.classifier !== "go" &&
      (!Number.isInteger(budget.testPackageDepth) ||
        budget.testPackageDepth < 0)
    ) {
      target.push(
        `source budget ${budget.id} must define a non-negative testPackageDepth`,
      );
    }
    try {
      isTestSource(
        budget.classifier,
        path.join(root, budget.directory, "policy-probe.js"),
        candidate.frontendArchitecture.testModuleMarkers,
      );
    } catch (error) {
      target.push(`source budget ${budget.id}: ${error.message}`);
    }
  }
  const positiveValues = {
    connectorSourceMaxLines: candidate.connectorSourceMaxLines,
    backendPackageDefaultMaxLines: candidate.backendPackage?.defaultMaxLines,
    frontendMaxDependencyFanout:
      candidate.frontendArchitecture?.maxDependencyFanout,
    frontendMaxProductionModuleLines:
      candidate.frontendArchitecture?.maxProductionModuleLines,
    goProductionMaxLines: candidate.goFunction?.productionMaxLines,
    goProductionMaxComplexity: candidate.goFunction?.productionMaxComplexity,
    goTestMaxLines: candidate.goFunction?.testMaxLines,
    goTestMaxComplexity: candidate.goFunction?.testMaxComplexity,
    backendPackageFanout: candidate.backendFanout?.packageMax,
    backendOwnerFanout: candidate.backendFanout?.ownerMax,
    backendOwnerFamilyFanout: candidate.backendFanout?.familyOwnerMax,
    backendTestPackageFanout: candidate.backendFanout?.testPackageMax,
    backendTestOwnerFanout: candidate.backendFanout?.testOwnerMax,
    backendTestOwnerFamilyFanout: candidate.backendFanout?.testFamilyOwnerMax,
    backendTestFileImports: candidate.backendFanout?.testFileInternalImportsMax,
    backendTestFileOwners: candidate.backendFanout?.testFileInternalOwnersMax,
  };
  for (const [name, value] of Object.entries(positiveValues)) {
    if (!positiveInteger(value))
      target.push(`${name} must be a positive integer`);
  }
  for (const [packagePath, floor] of Object.entries(
    candidate.backendCoverageFloors || {},
  )) {
    if (
      !packagePath.startsWith("internal/") ||
      typeof floor !== "number" ||
      floor <= 0 ||
      floor < candidate.backendCoverageDefaultFloor ||
      floor > 100
    ) {
      target.push(`invalid backend coverage floor ${packagePath}: ${floor}`);
    }
  }
  if (Object.keys(candidate.backendCoverageFloors || {}).length === 0) {
    target.push("backend coverage floors must not be empty");
  }
  if (
    typeof candidate.backendCoverageDefaultFloor !== "number" ||
    candidate.backendCoverageDefaultFloor <= 0 ||
    candidate.backendCoverageDefaultFloor > 100
  ) {
    target.push("backendCoverageDefaultFloor must be between 0 and 100");
  }
  const neutralCoverage = candidate.backendCoverageNeutralPackages || [];
  if (new Set(neutralCoverage).size !== neutralCoverage.length) {
    target.push("backend neutral coverage packages must be unique");
  }
  for (const packagePath of neutralCoverage) {
    if (
      !packagePath.startsWith("internal/") ||
      candidate.backendCoverageFloors?.[packagePath]
    ) {
      target.push(`invalid backend neutral coverage package ${packagePath}`);
    }
  }
  for (const [name, values] of [
    ["source override", candidate.sourceOverrides],
    [
      "backend package stricter ratchet",
      candidate.backendPackage?.stricterRatchets,
    ],
    ["backend fanout override", candidate.backendFanout?.overrides],
  ]) {
    for (const [key, value] of Object.entries(values || {})) {
      if (!positiveInteger(value))
        target.push(`${name} ${key} must be a positive integer`);
    }
  }
  for (const [name, values] of [
    ["source override", candidate.sourceOverrides],
    ["Go function override", candidate.goFunction?.overrides],
    ["backend fanout override", candidate.backendFanout?.overrides],
  ]) {
    if (Object.keys(values || {}).length > 0) {
      target.push(`${name} exceptions must remain empty`);
    }
    for (const key of Object.keys(values || {})) {
      if (isAPIExceptionPath(key)) {
        target.push(
          `${name} ${key} is forbidden; internal/api must satisfy shared budgets without exceptions`,
        );
      }
    }
  }
  return target;
}

function testPackageDirectory(budget, file) {
  if (budget.classifier === "go") return path.dirname(file);
  const budgetRoot = path.join(root, budget.directory);
  const directories = path
    .dirname(path.relative(budgetRoot, file))
    .split(path.sep)
    .filter((part) => part !== ".");
  return path.join(
    budgetRoot,
    ...directories.slice(0, budget.testPackageDepth),
  );
}

function checkSourceBudgets() {
  const overrides = new Map(Object.entries(policy.sourceOverrides));
  for (const budget of policy.sourceBudgets) {
    const packageLines = new Map();
    for (const file of matchingSources(budget)) {
      const test = testSource(budget, file);
      const lines = sourceLineCount(file);
      const relativePath = path.relative(root, file);
      if (test) {
        if (!budget.testMaxLines) continue;
        if (lines > budget.testMaxLines) {
          failures.push(
            `${relativePath} has ${lines} test lines; budget is ${budget.testMaxLines}`,
          );
        }
        const directory = testPackageDirectory(budget, file);
        packageLines.set(directory, (packageLines.get(directory) || 0) + lines);
        continue;
      }
      if (!budget.productionMaxLines) continue;
      let maxLines = overrides.get(relativePath) || budget.productionMaxLines;
      if (relativePath.startsWith("backend/internal/connectors/")) {
        maxLines = Math.min(maxLines, policy.connectorSourceMaxLines);
      }
      if (lines > maxLines) {
        failures.push(
          `${relativePath} has ${lines} lines; budget is ${maxLines}`,
        );
      }
    }
    if (!budget.testPackageMaxLines) continue;
    for (const [directory, lines] of packageLines) {
      if (lines > budget.testPackageMaxLines) {
        failures.push(
          `${path.relative(root, directory)} has ${lines} test lines; package test budget is ${budget.testPackageMaxLines}`,
        );
      }
    }
  }
}

function checkBackendPackageBudgets() {
  const packageLines = new Map();
  for (const file of walk(path.join(root, "backend"))) {
    if (path.extname(file) !== ".go" || file.endsWith("_test.go")) continue;
    const directory = path.dirname(file);
    packageLines.set(
      directory,
      (packageLines.get(directory) || 0) + sourceLineCount(file),
    );
  }
  for (const [directory, lines] of packageLines) {
    const relativePath = path.relative(root, directory);
    const maxLines =
      policy.backendPackage.stricterRatchets[relativePath] ||
      policy.backendPackage.defaultMaxLines;
    if (lines > maxLines) {
      failures.push(
        `${relativePath} has ${lines} production lines; package budget is ${maxLines}`,
      );
    }
  }
}

function checkFrontendSuppressions() {
  const suppressions = JSON.parse(
    fs.readFileSync(
      path.join(root, "frontend/eslint-suppressions.json"),
      "utf8",
    ),
  );
  let count = 0;
  for (const [file, rules] of Object.entries(suppressions)) {
    for (const value of Object.values(rules)) count += value.count || 0;
    if (policy.frontendSuppressions.forbiddenPaths.includes(file)) {
      failures.push(`${file} must not contain lint suppressions`);
    }
  }
  if (count > policy.frontendSuppressions.maxCount) {
    failures.push(
      `frontend hook suppressions total ${count}; budget is ${policy.frontendSuppressions.maxCount}`,
    );
  }
  return count;
}

function main() {
  validatePolicy();
  checkSourceBudgets();
  checkBackendPackageBudgets();
  const suppressionCount = checkFrontendSuppressions();

  if (failures.length > 0) {
    console.error("Maintenance budget check failed:");
    failures.forEach((failure) => console.error(`- ${failure}`));
    process.exit(1);
  }

  console.log(
    `Maintenance budgets passed: production/test source, package, tooling, and ${suppressionCount}/${policy.frontendSuppressions.maxCount} frontend hook suppressions.`,
  );
}

if (require.main === module) main();

module.exports = { testPackageDirectory, validatePolicy, walk };
