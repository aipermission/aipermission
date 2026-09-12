#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const frontendArchitecturePolicy = require("../frontend/architecture-policy.json");
const { isTestSource } = require("./maintenance-source-kind");
const frontendTestModuleMarkers = frontendArchitecturePolicy.testModuleMarkers;
const sourceBudgets = [
  { directory: "backend", extensions: new Set([".go"]), maxLines: 1400 },
  {
    directory: "frontend/src",
    extensions: new Set(frontendArchitecturePolicy.sourceExtensions),
    maxLines: frontendArchitecturePolicy.maxProductionModuleLines,
  },
  {
    directory: "packages/mcp/src",
    extensions: new Set([".js", ".ts"]),
    maxLines: 800,
  },
];
const sourceBudgetOverrides = new Map();
const connectorSourceBudget = 850;
const backendPackageBudget = 3500;
const backendTestSourceBudget = 1800;
const frontendTestSourceBudget = 1000;
const mcpTestSourceBudget = 800;
const backendTestPackageBudget = 15000;
const frontendTestPackageBudget = 3000;
const mcpTestPackageBudget = 1200;
const backendPackageBudgetOverrides = new Map([
  // Keep decomposed ownership boundaries from silently growing back toward
  // the global package ceiling.
  ["backend/internal/db", 2700],
  ["backend/internal/connectors/mail", 3200],
  ["backend/internal/console", 3300],
  ["backend/internal/connectors/s3", 3250],
]);
const suppressionBudget = 0;
const criticalSuppressionPaths = [
  "src/components/console/connector-token-permission-panel.jsx",
  "src/components/console/vault-session-dialog.jsx",
  "src/pages/console.jsx",
  "src/pages/vault.jsx",
];

const failures = [];

function sourceLineCount(file) {
  const source = fs.readFileSync(file, "utf8");
  if (source.length === 0) return 0;
  const lines = source.split("\n").length;
  return source.endsWith("\n") ? lines - 1 : lines;
}

function isProductionSource(directory, file) {
  return !isTestSource(directory, file, frontendTestModuleMarkers);
}

function walk(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(entryPath) : [entryPath];
  });
}

const testBudgets = [
  {
    directory: "backend",
    extensions: new Set([".go"]),
    maxSourceLines: backendTestSourceBudget,
    maxPackageLines: backendTestPackageBudget,
  },
  {
    directory: "frontend/src",
    extensions: new Set(frontendArchitecturePolicy.sourceExtensions),
    maxSourceLines: frontendTestSourceBudget,
    maxPackageLines: frontendTestPackageBudget,
  },
  {
    directory: "packages/mcp/test",
    extensions: new Set([".js", ".ts"]),
    maxSourceLines: mcpTestSourceBudget,
    maxPackageLines: mcpTestPackageBudget,
  },
];

for (const budget of testBudgets) {
  const packageLines = new Map();
  for (const file of walk(path.join(root, budget.directory))) {
    if (
      !budget.extensions.has(path.extname(file)) ||
      !isTestSource(budget.directory, file, frontendTestModuleMarkers)
    )
      continue;
    const lines = sourceLineCount(file);
    const relativePath = path.relative(root, file);
    if (lines > budget.maxSourceLines) {
      failures.push(
        `${relativePath} has ${lines} test lines; budget is ${budget.maxSourceLines}`,
      );
    }
    const directory = path.dirname(file);
    packageLines.set(directory, (packageLines.get(directory) || 0) + lines);
  }
  for (const [directory, lines] of packageLines) {
    if (lines > budget.maxPackageLines) {
      failures.push(
        `${path.relative(root, directory)} has ${lines} test lines; package test budget is ${budget.maxPackageLines}`,
      );
    }
  }
}

for (const budget of sourceBudgets) {
  const directory = path.join(root, budget.directory);
  for (const file of walk(directory)) {
    if (
      !budget.extensions.has(path.extname(file)) ||
      !isProductionSource(budget.directory, file)
    ) {
      continue;
    }
    const lines = sourceLineCount(file);
    const relativePath = path.relative(root, file);
    let maxLines = sourceBudgetOverrides.get(relativePath) || budget.maxLines;
    if (relativePath.startsWith("backend/internal/connectors/")) {
      maxLines = Math.min(maxLines, connectorSourceBudget);
    }
    if (lines > maxLines) {
      failures.push(
        `${relativePath} has ${lines} lines; budget is ${maxLines}`,
      );
    }
  }
}

const backendInternal = path.join(root, "backend/internal");
const packageLines = new Map();
for (const file of walk(backendInternal)) {
  if (path.extname(file) !== ".go" || !isProductionSource("backend", file)) {
    continue;
  }
  const directory = path.dirname(file);
  packageLines.set(
    directory,
    (packageLines.get(directory) || 0) + sourceLineCount(file),
  );
}
for (const [directory, lines] of packageLines) {
  const relativePath = path.relative(root, directory);
  const maxLines =
    backendPackageBudgetOverrides.get(relativePath) || backendPackageBudget;
  if (lines > maxLines) {
    failures.push(
      `${relativePath} has ${lines} production lines; package budget is ${maxLines}`,
    );
  }
}

const suppressionsPath = path.join(root, "frontend/eslint-suppressions.json");
const suppressions = JSON.parse(fs.readFileSync(suppressionsPath, "utf8"));
let suppressionCount = 0;
for (const [file, rules] of Object.entries(suppressions)) {
  for (const value of Object.values(rules)) {
    suppressionCount += value.count || 0;
  }
  if (criticalSuppressionPaths.includes(file)) {
    failures.push(`${file} must not contain lint suppressions`);
  }
}
if (suppressionCount > suppressionBudget) {
  failures.push(
    `frontend hook suppressions total ${suppressionCount}; budget is ${suppressionBudget}`,
  );
}

if (failures.length > 0) {
  console.error("Maintenance budget check failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exit(1);
}

console.log(
  `Maintenance budgets passed: production/test source, package, and ${suppressionCount}/${suppressionBudget} frontend hook suppressions.`,
);
