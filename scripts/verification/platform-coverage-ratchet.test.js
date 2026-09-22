const assert = require("node:assert/strict");
const test = require("node:test");

const policy = require("../../maintenance-policy.json");
const {
  budgetIncreases,
  policySnapshot,
} = require("../maintenance-budget-ratchet");

function copyPolicy() {
  return structuredClone(policy);
}

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
    "backend.coverage.platform.internal/arbitrary_windows.go.constraint.windows":
      -100,
  };
  assert.ok(
    budgetIncreases(legacy, arbitraryBootstrap).some((failure) =>
      failure.includes("arbitrary_windows"),
    ),
  );

  const expanded = {
    ...current,
    "backend.coverage.platform.internal/future_windows.go": 0,
    "backend.coverage.platform.internal/future_windows.go.constraint.windows":
      -100,
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

test("allows only fully covered platform sources with registered runtime evidence", () => {
  const base = policySnapshot(policy);
  const source = "internal/future/native_windows.go";
  const packagePath =
    "github.com/aipermission/aipermission/backend/internal/future";
  const testName = "TestNativeWindowsContract";

  const covered = copyPolicy();
  covered.windowsRuntimeTests.push({ package: packagePath, name: testName });
  covered.backendCoveragePlatformFiles[source] = {
    platform: "windows",
    buildConstraint: "windows",
    minimumCoverage: 100,
    tests: [{ package: packagePath, name: testName }],
  };
  assert.deepEqual(budgetIncreases(base, policySnapshot(covered)), []);

  const weak = structuredClone(covered);
  weak.backendCoveragePlatformFiles[source].minimumCoverage = 99;
  assert.ok(
    budgetIncreases(base, policySnapshot(weak)).some((failure) =>
      failure.includes("native_windows"),
    ),
  );

  const missingEvidence = structuredClone(covered);
  missingEvidence.backendCoveragePlatformFiles[source].tests = [];
  assert.ok(
    budgetIncreases(base, policySnapshot(missingEvidence)).some((failure) =>
      failure.includes("native_windows"),
    ),
  );

  const unregistered = structuredClone(covered);
  unregistered.windowsRuntimeTests.pop();
  assert.ok(
    budgetIncreases(base, policySnapshot(unregistered)).some((failure) =>
      failure.includes("native_windows"),
    ),
  );
});

test("allows fully covered Darwin sources with native runtime evidence", () => {
  const base = policySnapshot(policy);
  const source = "internal/future/native_darwin.go";
  const packagePath =
    "github.com/aipermission/aipermission/backend/internal/future";
  const testName = "TestNativeDarwinContract";
  const covered = copyPolicy();
  covered.darwinRuntimeTests.push({ package: packagePath, name: testName });
  covered.backendCoveragePlatformFiles[source] = {
    platform: "darwin",
    buildConstraint: "darwin",
    minimumCoverage: 100,
    tests: [{ package: packagePath, name: testName }],
  };
  assert.deepEqual(budgetIncreases(base, policySnapshot(covered)), []);
});
