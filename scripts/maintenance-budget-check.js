#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const sourceBudgets = [
  { directory: "backend", extensions: new Set([".go"]), maxLines: 1400 },
  {
    directory: "frontend/src",
    extensions: new Set([".js", ".jsx", ".ts", ".tsx"]),
    maxLines: 800,
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
const backendPackageBudgetOverrides = new Map([
  // The HTTP composition root is being decomposed release by release. Keep
  // this ceiling below its pre-v0.2.44 size and ratchet it down after moves.
  ["backend/internal/api", 23500],
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
  return fs.readFileSync(file, "utf8").split("\n").length;
}

function isProductionSource(file) {
  return !file.endsWith("_test.go") && !file.includes(".test.");
}

function walk(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(entryPath) : [entryPath];
  });
}

for (const budget of sourceBudgets) {
  const directory = path.join(root, budget.directory);
  for (const file of walk(directory)) {
    if (
      !budget.extensions.has(path.extname(file)) ||
      !isProductionSource(file)
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
  if (path.extname(file) !== ".go" || !isProductionSource(file)) {
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
  `Maintenance budgets passed: source, package, and ${suppressionCount}/${suppressionBudget} frontend hook suppressions.`,
);
