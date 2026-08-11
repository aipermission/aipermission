#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const sourceBudgets = [
  { directory: "backend", extensions: new Set([".go"]), maxLines: 1500 },
  { directory: "frontend/src", extensions: new Set([".js", ".jsx", ".ts", ".tsx"]), maxLines: 1800 },
  { directory: "packages/mcp/src", extensions: new Set([".js", ".ts"]), maxLines: 1200 },
];
const suppressionBudget = 0;
const criticalSuppressionPaths = [
  "src/components/console/connector-token-permission-panel.jsx",
  "src/components/console/vault-session-dialog.jsx",
  "src/pages/console.jsx",
  "src/pages/vault.jsx",
];

const failures = [];

function walk(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    return entry.isDirectory() ? walk(entryPath) : [entryPath];
  });
}

for (const budget of sourceBudgets) {
  const directory = path.join(root, budget.directory);
  for (const file of walk(directory)) {
    if (!budget.extensions.has(path.extname(file)) || file.endsWith("_test.go") || file.includes(".test.")) {
      continue;
    }
    const lines = fs.readFileSync(file, "utf8").split("\n").length;
    if (lines > budget.maxLines) {
      failures.push(`${path.relative(root, file)} has ${lines} lines; budget is ${budget.maxLines}`);
    }
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
  failures.push(`frontend hook suppressions total ${suppressionCount}; budget is ${suppressionBudget}`);
}

if (failures.length > 0) {
  console.error("Maintenance budget check failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exit(1);
}

console.log(`Maintenance budgets passed: ${suppressionCount}/${suppressionBudget} frontend hook suppressions.`);
