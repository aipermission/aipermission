const assert = require("node:assert/strict");
const test = require("node:test");

const policy = require("../maintenance-policy.json");
const { isTestSource } = require("./maintenance-source-kind");

const markers = policy.frontendArchitecture.testModuleMarkers;

test("classifies test modules according to the enforced policy", () => {
  assert.equal(isTestSource("go", "/tmp/service_test.go", markers), true);
  assert.equal(
    isTestSource("go", "/tmp/runtime.test.fixture.go", markers),
    false,
  );
  assert.equal(
    isTestSource("markers", "/tmp/runtime.test.fixture.js", markers),
    true,
  );
  assert.equal(
    isTestSource("markers", "/tmp/client.fixture.ts", markers),
    false,
  );
  assert.equal(isTestSource("all", "/tmp/helpers.js", markers), true);
});

test("MCP source tests and test support have an enforced test budget", () => {
  const byID = new Map(
    policy.sourceBudgets.map((budget) => [budget.id, budget]),
  );
  const source = byID.get("mcp-source");
  const support = byID.get("mcp-test");
  assert.equal(
    isTestSource(source.classifier, "example.test.js", markers),
    true,
  );
  assert.equal(isTestSource(support.classifier, "helpers.js", markers), true);
  assert.equal(source.testMaxLines, support.testMaxLines);
});

test("every maintenance tooling root has production and test budgets", () => {
  const byID = new Map(
    policy.sourceBudgets.map((budget) => [budget.id, budget]),
  );
  for (const id of ["repository-tooling", "frontend-tooling", "mcp-tooling"]) {
    assert.ok(byID.get(id)?.productionMaxLines, `${id} production budget`);
    assert.ok(byID.get(id)?.testMaxLines, `${id} test source budget`);
    assert.ok(byID.get(id)?.testPackageMaxLines, `${id} test package budget`);
  }
});

test("rejects unknown classifiers instead of silently weakening policy", () => {
  assert.throws(
    () => isTestSource("unknown", "/tmp/service.test.js", markers),
    /unknown maintenance source classifier/,
  );
});
