const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const policy = require("../../maintenance-policy.json");
const {
  testPackageDirectory,
  validatePolicy,
  walk,
} = require("../maintenance-budget-check");

const copyPolicy = () => structuredClone(policy);

test("rejects disabled budgets and broad test markers", () => {
  const candidate = copyPolicy();
  candidate.sourceBudgets[0].productionMaxLines = 0;
  candidate.backendFanout.familyOwnerMax = 0;
  candidate.frontendArchitecture.testModuleMarkers.push(".jsx");
  candidate.backendCoverageExceptionBaseline = 0;
  const failures = validatePolicy(candidate, []);
  assert.ok(failures.some((failure) => failure.includes("positive integer")));
  assert.ok(
    failures.some((failure) => failure.includes("markers must be exactly")),
  );
  assert.ok(failures.includes("backend coverage exception baseline must be 1"));
});

test("rejects invalid platform coverage evidence", () => {
  const candidate = copyPolicy();
  candidate.backendCoveragePlatformFiles["internal/db/../db/windows.go"] = {
    platform: "windows",
    buildConstraint: "windows",
    minimumCoverage: 100,
    tests: candidate.windowsRuntimeTests.slice(0, 1),
  };
  candidate.backendCoveragePlatformFiles["outside/windows.go"] = {
    platform: "linux",
    buildConstraint: "linux",
    minimumCoverage: 0,
    tests: [],
  };
  const failures = validatePolicy(candidate, []);
  for (const expected of [
    "invalid backend platform coverage source internal/db/../db/windows.go",
    "invalid backend platform coverage source outside/windows.go",
    "invalid backend platform coverage evidence outside/windows.go",
  ]) {
    assert.ok(failures.includes(expected));
  }
});

test("rejects normalized traversal in backend coverage owners", () => {
  const candidate = copyPolicy();
  candidate.backendCoverageFloors["internal/../outside"] = 7;
  candidate.backendCoverageNeutralPackages.push("cmd/../outside");
  const failures = validatePolicy(candidate, []);
  assert.ok(
    failures.some((failure) => failure.includes("internal/../outside")),
  );
  assert.ok(failures.some((failure) => failure.includes("cmd/../outside")));
});

test("rejects source ownership migrations that do not tighten the package cap", () => {
  const candidate = copyPolicy();
  candidate.sourceBudgetMigrations[0].toTestPackageMaxLines = 1500;
  assert.ok(
    validatePolicy(candidate, []).some((failure) =>
      failure.includes("invalid source budget migration"),
    ),
  );
});

test("accepts source ownership migration before and after its authorized move", () => {
  const before = copyPolicy();
  const budget = before.sourceBudgets.find(
    ({ id }) => id === "repository-tooling",
  );
  budget.testPackageDepth = 0;
  budget.testPackageMaxLines = 1500;
  assert.deepEqual(validatePolicy(before, []), []);
  assert.deepEqual(validatePolicy(copyPolicy(), []), []);
});

test("keeps nested javascript tests in their aggregate top-level owner", () => {
  const budget = {
    classifier: "markers",
    directory: "scripts",
    testPackageDepth: 1,
  };
  const root = path.resolve(__dirname, "../..");
  const owner = (file) =>
    testPackageDirectory(budget, path.join(root, `scripts/${file}`));
  assert.equal(
    owner("ci/policy/first.test.js"),
    owner("ci/platform/second.test.js"),
  );
  assert.equal(owner("ci/policy/first.test.js"), path.join(root, "scripts/ci"));
});

test("validates tooling, native platform evidence, and explicit coverage exclusions", () => {
  const candidate = copyPolicy();
  candidate.toolingTestFiles.push("scripts/../outside.test.js");
  candidate.windowsRuntimeTests.push(
    structuredClone(candidate.windowsRuntimeTests[0]),
  );
  candidate.darwinRuntimeTests.push(
    structuredClone(candidate.darwinRuntimeTests[0]),
  );
  candidate.backendCoveragePlatformFiles[
    "internal/db/ownership_windows.go"
  ].tests = [
    {
      package: "github.com/aipermission/aipermission/backend/internal/db",
      name: "TestMissing",
    },
  ];
  candidate.toolingTestRoots.push(candidate.toolingTestRoots[0]);
  candidate.backendCoverageExcludedPackages["internal/api"] = {
    context: "host",
    reason: "invalid",
  };
  const failures = validatePolicy(candidate, []);
  for (const expected of [
    "invalid tooling test inventory path",
    "Windows runtime test inventory must be unique",
    "Darwin runtime test inventory must be unique",
    "references an unregistered Windows test",
    "invalid backend coverage excluded package internal/api",
    "tooling test roots must be unique",
  ]) {
    assert.ok(
      failures.some((failure) => failure.includes(expected)),
      failures,
    );
  }
});

test("source walking excludes dependency trees at every depth", (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "maintenance-walk-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  fs.mkdirSync(path.join(directory, "nested", "node_modules"), {
    recursive: true,
  });
  fs.writeFileSync(path.join(directory, "source.js"), "source");
  fs.writeFileSync(
    path.join(directory, "nested", "node_modules", "dependency.js"),
    "dependency",
  );
  assert.deepEqual(walk(directory), [path.join(directory, "source.js")]);
});

test("rejects every internal API budget exception form", () => {
  const mutations = [
    (value) => {
      value.sourceOverrides["backend/internal/api/routes.go"] = 100;
    },
    (value) => {
      value.goFunction.overrides[
        "backend/internal/api/routes.go:registerRoutes"
      ] = { lines: 100, complexity: 20 };
    },
    (value) => {
      value.backendFanout.overrides[
        "github.com/aipermission/aipermission/backend/internal/api"
      ] = 10;
    },
  ];
  for (const mutate of mutations) {
    const candidate = copyPolicy();
    mutate(candidate);
    const failures = validatePolicy(candidate, []);
    assert.ok(
      failures.some((failure) =>
        failure.includes(
          "internal/api must satisfy shared budgets without exceptions",
        ),
      ),
      failures,
    );
  }
});
