const assert = require("node:assert/strict");
const test = require("node:test");

const { backendFanoutBudgets, budgetIncreases, budgetSnapshot, goFunctionBudgets } = require("./maintenance-budget-ratchet");

const checkSource = `
const sourceBudgets = [
  { directory: "backend", maxLines: 1400 },
  { directory: "packages/mcp/src", maxLines: 800 },
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
  ["backend/internal/api", 23500],
]);
const suppressionBudget = 0;
`;
const architectureSource = JSON.stringify({ maxDependencyFanout: 20, maxProductionModuleLines: 550 });

test("extracts every mutable maintenance ceiling", () => {
  assert.deepEqual(budgetSnapshot(checkSource, architectureSource), {
    "frontend.maxDependencyFanout": 20,
    "frontend.maxProductionModuleLines": 550,
    connectorSourceBudget: 850,
    backendPackageBudget: 3500,
    backendTestSourceBudget: 1800,
    frontendTestSourceBudget: 1000,
    mcpTestSourceBudget: 800,
    backendTestPackageBudget: 15000,
    frontendTestPackageBudget: 3000,
    mcpTestPackageBudget: 1200,
    suppressionBudget: 0,
    "source.backend.maxLines": 1400,
    "source.mcp.maxLines": 800,
    "backend.package.backend/internal/api": 23500,
  });
});

test("treats newly introduced test budgets as tightening", () => {
  const legacy = checkSource
    .split("\n")
    .filter((line) => !line.includes("TestSourceBudget") && !line.includes("TestPackageBudget"))
    .join("\n");
  assert.deepEqual(budgetIncreases(budgetSnapshot(legacy, architectureSource), budgetSnapshot(checkSource, architectureSource)), []);
});

test("rejects raised ceilings while allowing tighter inherited package budgets", () => {
  const base = budgetSnapshot(checkSource, architectureSource);
  assert.deepEqual(budgetIncreases(base, { ...base, connectorSourceBudget: 849 }), []);
  assert.deepEqual(budgetIncreases(base, { ...base, connectorSourceBudget: 851 }), ["connectorSourceBudget increased from 850 to 851"]);
  assert.deepEqual(budgetIncreases(base, { ...base, "backend.package.backend/internal/new": 100 }), []);
  assert.deepEqual(budgetIncreases(base, { ...base, "backend.package.backend/internal/new": 3600 }), [
    "backend.package.backend/internal/new is a new unreviewed budget (3600)",
  ]);
});

test("rejects a source-file exception introduced by the same change", () => {
  const base = budgetSnapshot(checkSource, architectureSource);
  const tighter = checkSource.replace("const sourceBudgetOverrides = new Map();", 'const sourceBudgetOverrides = new Map([["frontend/src/small.jsx", 500]]);');
  assert.deepEqual(budgetIncreases(base, budgetSnapshot(tighter, architectureSource)), []);
  const changed = checkSource.replace("const sourceBudgetOverrides = new Map();", 'const sourceBudgetOverrides = new Map([["frontend/src/large.jsx", 900]]);');
  assert.deepEqual(budgetIncreases(base, budgetSnapshot(changed, architectureSource)), [
    "source.override.frontend/src/large.jsx is a new unreviewed budget (900)",
  ]);
});

test("treats architecture constraints added over the legacy source budget as tightening", () => {
  const legacySource = checkSource.replace(
    '{ directory: "packages/mcp/src", maxLines: 800 },',
    '{ directory: "frontend/src", maxLines: 800 },\n  { directory: "packages/mcp/src", maxLines: 800 },',
  );
  const legacy = budgetSnapshot(legacySource);
  const current = budgetSnapshot(legacySource, architectureSource);
  assert.equal(legacy["frontend.maxDependencyFanout"], Number.POSITIVE_INFINITY);
  assert.deepEqual(budgetIncreases(legacy, current), []);
});

test("ratchets Go function defaults and per-function overrides", () => {
  const source = `
const (
  defaultMaxLines = 180
  defaultMaxComplexity = 35
)
var overrides = map[string]budget{
  "internal/api/routes.go:Server.routes": {lines: 191, complexity: defaultMaxComplexity},
}
`;
  const snapshot = goFunctionBudgets(source);
  assert.deepEqual(snapshot, {
    "go.function.default.lines": 180,
    "go.function.default.complexity": 35,
    "go.function.override.internal/api/routes.go:Server.routes.lines": 191,
    "go.function.override.internal/api/routes.go:Server.routes.complexity": 35,
  });
  assert.deepEqual(budgetIncreases(snapshot, { ...snapshot, "go.function.default.lines": 181 }), [
    "go.function.default.lines increased from 180 to 181",
  ]);
  assert.deepEqual(
    goFunctionBudgets(`
const (
  defaultMaxLines = 180
  defaultMaxComplexity = 35
)
`),
    {
      "go.function.default.lines": 180,
      "go.function.default.complexity": 35,
    },
  );
});

test("ratchets backend fan-out defaults and overrides", () => {
  const source = `
func test() {
  const defaultBudget = 8
  overrides := map[string]int{
    modulePath + "/internal/api": 28,
  }
}
`;
  assert.deepEqual(backendFanoutBudgets(source), {
    "go.fanout.default": 8,
    "go.fanout.override./internal/api": 28,
  });
});
