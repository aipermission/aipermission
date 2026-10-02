const assert = require("node:assert/strict");
const test = require("node:test");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { budgetHeadroom } = require("./budget-headroom");
const { copyPolicy, temporaryRoot, writeFixtureFiles } = require("./test-fixtures");

test("warns at exact 85/90 percent boundaries without relaxing hard limits", () => {
  for (const [used, expected] of [
    [0, null],
    [84, null],
    [85, 85],
    [89, 85],
    [90, 90],
    [100, 90],
    [101, null],
  ]) {
    const result = budgetHeadroom("owner", used, 100);
    if (expected === null) assert.equal(result, null);
    else assert.equal(result, `owner: ${used}/100 lines (${expected}% headroom warning); hard limit unchanged`);
  }
});

test("does not round a below-threshold measurement into a warning", () => {
  assert.equal(budgetHeadroom("owner", 169, 200), null);
  assert.match(budgetHeadroom("owner", 170, 200), /85%/);
  assert.match(budgetHeadroom("owner", 179, 200), /85%/);
  assert.match(budgetHeadroom("owner", 180, 200), /90%/);
});

test("rejects invalid measurement inputs instead of hiding budget errors", () => {
  for (const [used, limit] of [
    [-1, 100],
    [1.5, 100],
    [1, 0],
    [1, -1],
    [1, NaN],
    [NaN, 1],
  ]) {
    assert.throws(() => budgetHeadroom("owner", used, limit), TypeError);
  }
});

test("the real checker warns before the limit and still fails above it", (t) => {
  const repository = path.resolve(__dirname, "../..");
  const scripts = ["maintenance-budget-check.js", "maintenance-source-kind.js", "maintenance/budget-headroom.js"];
  const policy = copyPolicy();
  policy.sourceBudgets = [
    policy.sourceBudgets.find(({ id }) => id === "repository-tooling"),
    {
      id: "probe",
      directory: "backend",
      extensions: [".go"],
      classifier: "go",
      productionMaxLines: 100,
      testMaxLines: 100,
      testPackageMaxLines: 100,
    },
  ];
  policy.sourceBudgetMigrations = [];
  policy.toolingTestRoots = ["scripts/maintenance"];
  policy.toolingTestFiles = ["scripts/maintenance/budget-headroom.test.js"];
  policy.backendPackage = { defaultMaxLines: 100, stricterRatchets: {} };
  const root = temporaryRoot(t, {
    ...Object.fromEntries(scripts.map((name) => [`scripts/${name}`, { copyFrom: path.join(repository, "scripts", name) }])),
    [policy.toolingTestFiles[0]]: { copyFrom: __filename },
    "maintenance-policy.json": JSON.stringify(policy),
    "frontend/eslint-suppressions.json": {
      copyFrom: path.join(repository, "frontend/eslint-suppressions.json"),
    },
  });
  const invoke = (lines) => {
    writeFixtureFiles(root, { "backend/probe.go": "line\n".repeat(lines) });
    return spawnSync(process.execPath, [path.join(root, "scripts/maintenance-budget-check.js")], { encoding: "utf8" });
  };
  const below = invoke(84);
  assert.equal(below.status, 0, below.stderr);
  assert.doesNotMatch(below.stderr, /Budget headroom: backend/);
  const warning = invoke(85);
  assert.equal(warning.status, 0, warning.stderr);
  assert.match(warning.stderr, /85% headroom warning/);
  const highWarning = invoke(90);
  assert.equal(highWarning.status, 0, highWarning.stderr);
  assert.match(highWarning.stderr, /90% headroom warning/);
  const exceeded = invoke(101);
  assert.equal(exceeded.status, 1);
  assert.match(exceeded.stderr, /has 101 lines; budget is 100/);
  assert.match(exceeded.stderr, /has 101 production lines; package budget is 100/);
  assert.doesNotMatch(exceeded.stderr, /Budget headroom: backend/);
});
