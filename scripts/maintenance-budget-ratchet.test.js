const assert = require("node:assert/strict");
const test = require("node:test");

const policy = require("../maintenance-policy.json");
const {
  budgetIncreases,
  legacyBudgetSnapshot,
  policySnapshot,
} = require("./maintenance-budget-ratchet");

function copyPolicy() {
  return JSON.parse(JSON.stringify(policy));
}

test("reads the exact machine policy consumed by enforcement", () => {
  const snapshot = policySnapshot(policy);
  assert.equal(snapshot["source.backend.maxLines"], 1400);
  assert.equal(snapshot["go.function.default.lines"], 180);
  assert.equal(snapshot["go.fanout.package"], 12);
  assert.equal(snapshot.repositoryToolingTestPackageBudget, 1500);
  assert.equal(snapshot["test.package.depth.frontend"], 3);
});

test("source-code decoys cannot influence the canonical policy snapshot", () => {
  const decoy = `const backendPackageBudget = 999999;\nconst defaultMaxLines = 999999;`;
  assert.equal(policySnapshot(policy).backendPackageBudget, 3500);
  assert.equal(policySnapshot(policy)["go.function.default.lines"], 180);
  assert.ok(decoy.includes("999999"));
});

test("rejects relaxed numeric budgets", () => {
  const current = copyPolicy();
  current.backendPackage.defaultMaxLines++;
  assert.deepEqual(
    budgetIncreases(policySnapshot(policy), policySnapshot(current)),
    ["backendPackageBudget increased from 3500 to 3501"],
  );
});

test("rejects test package depth increases", () => {
  const current = copyPolicy();
  const frontend = current.sourceBudgets.find(
    (budget) => budget.id === "frontend",
  );
  frontend.testPackageDepth++;
  assert.deepEqual(
    budgetIncreases(policySnapshot(policy), policySnapshot(current)),
    ["test.package.depth.frontend increased from 3 to 4"],
  );
});

test("rejects removed coverage roots, extensions, markers, and classifiers", () => {
  const current = copyPolicy();
  current.frontendArchitecture.testModuleMarkers.pop();
  current.sourceBudgets = current.sourceBudgets.filter(
    (budget) => budget.id !== "mcp-test",
  );
  const failures = budgetIncreases(
    policySnapshot(policy),
    policySnapshot(current),
  );
  assert.ok(
    failures.some((failure) => failure.includes("coverage.test.marker")),
  );
  assert.ok(
    failures.some((failure) => failure.includes("coverage.source.mcp-test")),
  );
});

test("allows new covered roots only within bootstrap ceilings", () => {
  const base = policySnapshot(policy);
  const tighter = copyPolicy();
  tighter.sourceBudgets.push({
    id: "new-tooling",
    directory: "new/scripts",
    extensions: [".js"],
    classifier: "markers",
    productionMaxLines: 100,
    testMaxLines: 100,
    testPackageMaxLines: 100,
  });
  const failures = budgetIncreases(base, policySnapshot(tighter));
  assert.ok(failures.some((failure) => failure.includes("new-tooling")));
});

test("allows removing a loose exception but preserves a tight exception", () => {
  const base = policySnapshot(policy);
  const loose = { ...base, "backend.package.backend/internal/large": 4000 };
  assert.deepEqual(budgetIncreases(loose, base), []);
  const tight = { ...base, "backend.package.backend/internal/small": 2700 };
  assert.deepEqual(budgetIncreases(tight, base), [
    "backend.package.backend/internal/small was removed from the current maintenance budget",
  ]);
});

test("legacy migration reads old effective limits once", () => {
  const check = `
const connectorSourceBudget = 850;
const backendPackageBudget = 3500;
const suppressionBudget = 0;
const backendTestSourceBudget = 1800;
const frontendTestSourceBudget = 1000;
const mcpTestSourceBudget = 800;
const backendTestPackageBudget = 15000;
const frontendTestPackageBudget = 3000;
const mcpTestPackageBudget = 1200;
const sourceBudgets = [
  { directory: "backend", maxLines: 1400 },
  { directory: "packages/mcp/src", maxLines: 800 },
];`;
  const architecture = JSON.stringify({
    sourceExtensions: [".js"],
    testModuleMarkers: [".test."],
    maxDependencyFanout: 20,
    maxProductionModuleLines: 550,
  });
  const functions = `const defaultMaxLines = 180; const defaultMaxComplexity = 35; const defaultMaxTestLines = 220; const defaultMaxTestComplexity = 60;`;
  const backend = `const packageBudget = 12; const ownerBudget = 8; const maxTestFileInternalImports = 14;`;
  const snapshot = legacyBudgetSnapshot(
    check,
    architecture,
    functions,
    backend,
  );
  assert.equal(snapshot["source.backend.maxLines"], 1400);
  assert.equal(snapshot["go.fanout.owner"], 8);
  assert.equal(snapshot["coverage.test.marker..test."], 0);
});
