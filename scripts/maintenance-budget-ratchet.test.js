const assert = require("node:assert/strict");
const test = require("node:test");

const {
  backendFanoutBudgets,
  budgetIncreases,
  budgetSnapshot,
  goFunctionBudgets,
} = require("./maintenance-budget-ratchet");

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
const architectureSource = JSON.stringify({
  maxDependencyFanout: 20,
  maxProductionModuleLines: 550,
});

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
    .filter(
      (line) =>
        !line.includes("TestSourceBudget") &&
        !line.includes("TestPackageBudget"),
    )
    .join("\n");
  assert.deepEqual(
    budgetIncreases(
      budgetSnapshot(legacy, architectureSource),
      budgetSnapshot(checkSource, architectureSource),
    ),
    [],
  );
});

test("rejects newly introduced test budgets above bootstrap ceilings", () => {
  const legacy = checkSource
    .split("\n")
    .filter(
      (line) =>
        !line.includes("TestSourceBudget") &&
        !line.includes("TestPackageBudget"),
    )
    .join("\n");
  const relaxed = checkSource.replace(
    "const backendTestSourceBudget = 1800;",
    "const backendTestSourceBudget = 1801;",
  );
  assert.deepEqual(
    budgetIncreases(
      budgetSnapshot(legacy, architectureSource),
      budgetSnapshot(relaxed, architectureSource),
    ),
    ["backendTestSourceBudget is a new unreviewed budget (1801)"],
  );
});

test("rejects removing an established maintenance ceiling", () => {
  const base = budgetSnapshot(checkSource, architectureSource);
  const withoutTestCeiling = checkSource.replace(
    "const frontendTestSourceBudget = 1000;",
    "",
  );
  assert.deepEqual(
    budgetIncreases(
      base,
      budgetSnapshot(withoutTestCeiling, architectureSource),
    ),
    [
      "frontendTestSourceBudget was removed from the current maintenance budget",
    ],
  );
});

test("allows removing an explicit exception because the default becomes stricter", () => {
  const base = budgetSnapshot(checkSource, architectureSource);
  const withoutOverride = checkSource.replace(
    'const backendPackageBudgetOverrides = new Map([\n  ["backend/internal/api", 23500],\n]);',
    "const backendPackageBudgetOverrides = new Map();",
  );
  assert.deepEqual(
    budgetIncreases(base, budgetSnapshot(withoutOverride, architectureSource)),
    [],
  );
});

test("rejects removing an exception when its inherited budget would be looser", () => {
  const packageBase = {
    backendPackageBudget: 3500,
    "backend.package.backend/internal/small": 2700,
  };
  assert.deepEqual(
    budgetIncreases(packageBase, { backendPackageBudget: 3500 }),
    [
      "backend.package.backend/internal/small was removed from the current maintenance budget",
    ],
  );

  const functionBase = {
    "go.function.default.lines": 180,
    "go.function.default.complexity": 35,
    "go.function.override.internal/small.go:small.lines": 120,
    "go.function.override.internal/small.go:small.complexity": 20,
  };
  assert.deepEqual(
    budgetIncreases(functionBase, {
      "go.function.default.lines": 180,
      "go.function.default.complexity": 35,
    }),
    [
      "go.function.override.internal/small.go:small.lines was removed from the current maintenance budget",
      "go.function.override.internal/small.go:small.complexity was removed from the current maintenance budget",
    ],
  );

  const fanoutBase = {
    "go.fanout.package": 12,
    "go.fanout.override./internal/small": 7,
  };
  assert.deepEqual(budgetIncreases(fanoutBase, { "go.fanout.package": 12 }), [
    "go.fanout.override./internal/small was removed from the current maintenance budget",
  ]);
});

test("rejects raised ceilings while allowing tighter inherited package budgets", () => {
  const base = budgetSnapshot(checkSource, architectureSource);
  assert.deepEqual(
    budgetIncreases(base, { ...base, connectorSourceBudget: 849 }),
    [],
  );
  assert.deepEqual(
    budgetIncreases(base, { ...base, connectorSourceBudget: 851 }),
    ["connectorSourceBudget increased from 850 to 851"],
  );
  assert.deepEqual(
    budgetIncreases(base, {
      ...base,
      "backend.package.backend/internal/new": 100,
    }),
    [],
  );
  assert.deepEqual(
    budgetIncreases(base, {
      ...base,
      "backend.package.backend/internal/new": 3600,
    }),
    ["backend.package.backend/internal/new is a new unreviewed budget (3600)"],
  );
});

test("rejects a source-file exception introduced by the same change", () => {
  const base = budgetSnapshot(checkSource, architectureSource);
  const tighter = checkSource.replace(
    "const sourceBudgetOverrides = new Map();",
    'const sourceBudgetOverrides = new Map([["frontend/src/small.jsx", 500]]);',
  );
  assert.deepEqual(
    budgetIncreases(base, budgetSnapshot(tighter, architectureSource)),
    [],
  );
  const changed = checkSource.replace(
    "const sourceBudgetOverrides = new Map();",
    'const sourceBudgetOverrides = new Map([["frontend/src/large.jsx", 900]]);',
  );
  assert.deepEqual(
    budgetIncreases(base, budgetSnapshot(changed, architectureSource)),
    ["source.override.frontend/src/large.jsx is a new unreviewed budget (900)"],
  );
});

test("treats architecture constraints added over the legacy source budget as tightening", () => {
  const legacySource = checkSource.replace(
    '{ directory: "packages/mcp/src", maxLines: 800 },',
    '{ directory: "frontend/src", maxLines: 800 },\n  { directory: "packages/mcp/src", maxLines: 800 },',
  );
  const legacy = budgetSnapshot(legacySource);
  const current = budgetSnapshot(legacySource, architectureSource);
  assert.equal(
    legacy["frontend.maxDependencyFanout"],
    Number.POSITIVE_INFINITY,
  );
  assert.deepEqual(budgetIncreases(legacy, current), []);
});

test("ratchets Go function defaults and per-function overrides", () => {
  const source = `
const (
  defaultMaxLines = 180
  defaultMaxComplexity = 35
  defaultMaxTestLines = 220
  defaultMaxTestComplexity = 60
)
var overrides = map[string]budget{
  "internal/api/routes.go:Server.routes": {lines: 191, complexity: defaultMaxComplexity},
}
`;
  const snapshot = goFunctionBudgets(source);
  assert.deepEqual(snapshot, {
    "go.function.default.lines": 180,
    "go.function.default.complexity": 35,
    "go.function.test.lines": 220,
    "go.function.test.complexity": 60,
    "go.function.override.internal/api/routes.go:Server.routes.lines": 191,
    "go.function.override.internal/api/routes.go:Server.routes.complexity": 35,
  });
  assert.deepEqual(
    budgetIncreases(snapshot, {
      ...snapshot,
      "go.function.default.lines": 181,
    }),
    ["go.function.default.lines increased from 180 to 181"],
  );
  assert.deepEqual(
    budgetIncreases(snapshot, {
      ...snapshot,
      "go.function.test.complexity": 61,
    }),
    ["go.function.test.complexity increased from 60 to 61"],
  );
  assert.deepEqual(
    goFunctionBudgets(`
const (
  defaultMaxLines = 180
  defaultMaxComplexity = 35
  defaultMaxTestLines = 220
  defaultMaxTestComplexity = 60
)
`),
    {
      "go.function.default.lines": 180,
      "go.function.default.complexity": 35,
      "go.function.test.lines": 220,
      "go.function.test.complexity": 60,
    },
  );
});

test("ratchets backend fan-out defaults and overrides", () => {
  const source = `
func test() {
  const defaultBudget = 8
  const maxTestFileInternalImports = 14
  overrides := map[string]int{
    modulePath + "/internal/api": 28,
  }
}
`;
  assert.deepEqual(backendFanoutBudgets(source), {
    "go.fanout.package": 8,
    "go.testImports.maxPerFile": 14,
    "go.fanout.override./internal/api": 28,
  });
});

test("ratchets package and owner fan-out budgets without legacy overrides", () => {
  const source = `
func test() {
  const packageBudget = 11
  const ownerBudget = 8
  const maxTestFileInternalImports = 14
}
`;
  assert.deepEqual(backendFanoutBudgets(source), {
    "go.fanout.package": 11,
    "go.fanout.owner": 8,
    "go.testImports.maxPerFile": 14,
  });
});

test("allows a package fan-out migration only when the new owner budget stays tight", () => {
  const base = { "go.fanout.package": 8 };
  assert.deepEqual(
    budgetIncreases(base, {
      "go.fanout.package": 11,
      "go.fanout.owner": 8,
    }),
    [],
  );
  assert.deepEqual(
    budgetIncreases(base, {
      "go.fanout.package": 11,
      "go.fanout.owner": 9,
    }),
    [
      "go.fanout.package increased from 8 to 11",
      "go.fanout.owner is a new unreviewed budget (9)",
    ],
  );
});
