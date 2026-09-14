const assert = require("node:assert/strict");
const test = require("node:test");

const policy = require("../../maintenance-policy.json");
const {
  budgetIncreases,
  legacyBudgetSnapshot,
  policySnapshot,
  resolveBaseReference,
} = require("../maintenance-budget-ratchet");

function copyPolicy() {
  return JSON.parse(JSON.stringify(policy));
}

test("reads the exact machine policy consumed by enforcement", () => {
  const snapshot = policySnapshot(policy);
  assert.equal(snapshot["source.backend.maxLines"], 1400);
  assert.equal(snapshot["go.function.default.lines"], 180);
  assert.equal(snapshot["go.fanout.package"], 12);
  assert.equal(snapshot["go.fanout.ownerFamily"], 25);
  assert.equal(snapshot["go.fanout.test.package"], 48);
  assert.equal(snapshot["go.fanout.test.ownerFamily"], 43);
  assert.equal(snapshot.repositoryToolingTestPackageBudget, 900);
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

test("bootstraps and ratchets the aggregate owner-family budget", () => {
  const current = policySnapshot(policy);
  const legacy = { ...current };
  delete legacy["go.fanout.ownerFamily"];
  assert.deepEqual(budgetIncreases(legacy, current), []);

  const relaxed = { ...current, "go.fanout.ownerFamily": 26 };
  assert.deepEqual(budgetIncreases(current, relaxed), [
    "go.fanout.ownerFamily increased from 25 to 26",
  ]);
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

test("keeps repository tooling under one aggregate owner budget", () => {
  const snapshot = policySnapshot(policy);
  assert.equal(snapshot["test.package.depth.repository-tooling"], 1);
  assert.equal(snapshot.repositoryToolingTestPackageBudget, 900);
});

test("permits the declared ownership split from a bootstrap ceiling", () => {
  const current = {
    "test.package.depth.repository-tooling": 1,
    repositoryToolingTestPackageBudget: 900,
    "migration.test.package.repository-tooling.0.1.1500.900": 0,
  };
  assert.deepEqual(budgetIncreases({}, current), []);
  const arbitrary = {
    "test.package.depth.repository-tooling": 2,
    repositoryToolingTestPackageBudget: 800,
    "migration.test.package.repository-tooling.0.2.1500.800": 0,
  };
  assert.ok(
    budgetIncreases({}, arbitrary).some((failure) => failure.includes("depth")),
  );
  delete current["migration.test.package.repository-tooling.0.1.1500.900"];
  assert.deepEqual(budgetIncreases({}, current), [
    "test.package.depth.repository-tooling is a new unreviewed budget (1)",
  ]);
});

test("requires ownership migrations to be pre-authorized by the trusted base", () => {
  const marker = "migration.test.package.repository-tooling.1.2.900.800";
  const before = {
    "test.package.depth.repository-tooling": 1,
    repositoryToolingTestPackageBudget: 900,
  };
  const selfAuthorized = {
    ...before,
    "test.package.depth.repository-tooling": 2,
    repositoryToolingTestPackageBudget: 800,
    [marker]: 0,
  };
  assert.ok(
    budgetIncreases(before, selfAuthorized).some((failure) =>
      failure.includes("depth"),
    ),
  );
  const authorized = { ...before, [marker]: 0 };
  assert.deepEqual(budgetIncreases(before, authorized), []);
  assert.deepEqual(budgetIncreases(authorized, selfAuthorized), []);
});

test("permits removing a completed ownership migration marker", () => {
  const base = policySnapshot(policy);
  const current = { ...base };
  delete current[
    Object.keys(current).find((key) =>
      key.startsWith("migration.test.package.repository-tooling."),
    )
  ];
  assert.deepEqual(budgetIncreases(base, current), []);
});

test("ratchets tooling and Windows runtime test inventory", () => {
  const base = policySnapshot(policy);
  const added = copyPolicy();
  added.toolingTestFiles.push("scripts/ci/future.test.js");
  added.windowsRuntimeTests.push({
    package: "github.com/aipermission/aipermission/backend/internal/future",
    name: "TestFutureRuntime",
  });
  assert.deepEqual(budgetIncreases(base, policySnapshot(added)), []);

  const removed = copyPolicy();
  removed.toolingTestFiles.pop();
  removed.windowsRuntimeTests.pop();
  const failures = budgetIncreases(base, policySnapshot(removed));
  assert.ok(
    failures.some((failure) => failure.includes("test.tooling.inventory")),
  );
  assert.ok(
    failures.some((failure) => failure.includes("test.windows.runtime")),
  );
});

test("bootstraps platform exclusions once and rejects later expansion", () => {
  const current = policySnapshot(policy);
  const legacy = { ...current };
  delete legacy["backend.coverage.exceptionBaseline"];
  for (const key of Object.keys(legacy)) {
    if (
      key.startsWith("backend.coverage.platform.") ||
      key.startsWith("backend.coverage.excluded.")
    ) {
      delete legacy[key];
    }
  }
  assert.deepEqual(budgetIncreases(legacy, current), []);

  const arbitraryBootstrap = {
    ...current,
    "backend.coverage.platform.internal/arbitrary_windows.go": 0,
    "backend.coverage.platform.internal/arbitrary_windows.go.constraint.windows": -100,
  };
  assert.ok(
    budgetIncreases(legacy, arbitraryBootstrap).some((failure) =>
      failure.includes("arbitrary_windows"),
    ),
  );

  const expanded = {
    ...current,
    "backend.coverage.platform.internal/future_windows.go": 0,
    "backend.coverage.platform.internal/future_windows.go.constraint.windows": -100,
    "backend.coverage.excluded.cmd/future": 0,
  };
  const expansionFailures = budgetIncreases(current, expanded);
  assert.ok(
    expansionFailures.some((failure) => failure.includes("future_windows")),
  );
  assert.ok(
    expansionFailures.some((failure) => failure.includes("cmd/future")),
  );

  const reduced = { ...current };
  delete reduced[
    Object.keys(reduced).find((key) =>
      key.startsWith("backend.coverage.platform."),
    )
  ];
  delete reduced[
    Object.keys(reduced).find((key) =>
      key.startsWith("backend.coverage.excluded."),
    )
  ];
  assert.deepEqual(budgetIncreases(current, reduced), []);

  const replayed = {
    ...reduced,
    "backend.coverage.excluded.cmd/future": 0,
  };
  assert.ok(
    budgetIncreases(reduced, replayed).some((failure) =>
      failure.includes("cmd/future"),
    ),
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

test("ratchets backend coverage floor membership and values", () => {
  const base = policySnapshot(policy);
  const lower = copyPolicy();
  lower.backendCoverageFloors["internal/api"]--;
  assert.deepEqual(budgetIncreases(base, policySnapshot(lower)), [
    "coverage.backend.floor.internal/api increased from -57 to -56",
  ]);

  const removed = copyPolicy();
  delete removed.backendCoverageFloors["internal/api"];
  assert.deepEqual(budgetIncreases(base, policySnapshot(removed)), [
    "coverage.backend.floor.internal/api was removed from the current maintenance budget",
  ]);

  const added = copyPolicy();
  added.backendCoverageFloors["internal/new-owner"] = 10;
  assert.deepEqual(budgetIncreases(base, policySnapshot(added)), []);
  const migrated = {
    "coverage.backend.default": -1,
    "coverage.backend.floor.internal/new-owner": -10,
  };
  assert.deepEqual(budgetIncreases({}, migrated), []);

  const belowDefault = copyPolicy();
  belowDefault.backendCoverageFloors["internal/new-owner"] = 0.001;
  assert.deepEqual(budgetIncreases(base, policySnapshot(belowDefault)), [
    "coverage.backend.floor.internal/new-owner is a new unreviewed budget (-0.001)",
  ]);
  migrated["coverage.backend.floor.internal/new-owner"] = -0.001;
  assert.deepEqual(budgetIncreases({}, migrated), [
    "coverage.backend.floor.internal/new-owner is a new unreviewed budget (-0.001)",
  ]);

  const neutral = copyPolicy();
  neutral.backendCoverageNeutralPackages.push("internal/new-owner");
  assert.deepEqual(budgetIncreases(base, policySnapshot(neutral)), [
    "backend.coverage.neutral.internal/new-owner is a new unreviewed budget (0)",
  ]);
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

test("maintenance budget base fails closed instead of comparing HEAD to itself", () => {
  const fakeGit = (...args) => {
    if (args[0] === "merge-base" && args[1] === "HEAD") return "base-ref";
    if (args[0] === "rev-parse" && args[1] === "base-ref^{commit}")
      return "base-sha";
    if (args[0] === "rev-parse" && args[1] === "configured^{commit}")
      return "base-sha";
    if (args[0] === "rev-parse" && args[1] === "HEAD^{commit}")
      return "head-sha";
    if (args[0] === "merge-base" && args[1] === "--is-ancestor") return "";
    throw new Error(`unexpected git command: ${args.join(" ")}`);
  };
  assert.throws(
    () =>
      resolveBaseReference("HEAD", (...args) => {
        if (args[0] === "rev-parse") return "same-sha";
        return "";
      }, {}),
    /must not resolve to HEAD/,
  );
  assert.throws(
    () => resolveBaseReference("000000", () => "", {}),
    /must identify a non-zero base commit/,
  );
  assert.equal(resolveBaseReference("", fakeGit, {}), "base-sha");
  assert.equal(resolveBaseReference("configured", fakeGit, {}), "base-sha");
});
