const assert = require("node:assert/strict");
const test = require("node:test");

const policy = require("../../maintenance-policy.json");
const { isTestSource } = require("../maintenance-source-kind");

const markers = policy.frontendArchitecture.testModuleMarkers;
const budgets = new Map(
  policy.sourceBudgets.map((budget) => [budget.id, budget]),
);

test("classifies test modules according to the enforced policy", () => {
  const cases = [
    ["go", "/tmp/service_test.go", true],
    ["go", "/tmp/runtime.test.fixture.go", false],
    ["markers", "/tmp/runtime.test.fixture.js", false],
    ["markers", "/tmp/cache.test.fixtures/production.js", false],
    ["markers", "C:\\tmp\\cache.spec.fixtures\\production.js", false],
    ["markers", "/tmp/client.fixture.ts", false],
    ["markers", "/tmp/runtime.test.js", true],
    ["markers", "/tmp/runtime.spec.mjs", true],
    ["markers", "/tmp/runtime.test.facade.js", false],
    ["all", "/tmp/helpers.js", true],
  ];
  for (const [classifier, path, expected] of cases) {
    assert.equal(isTestSource(classifier, path, markers), expected, path);
  }
});

test("MCP source tests and test support have an enforced test budget", () => {
  const source = budgets.get("mcp-source");
  const support = budgets.get("mcp-test");
  assert.equal(
    isTestSource(source.classifier, "example.test.js", markers),
    true,
  );
  assert.equal(isTestSource(support.classifier, "helpers.js", markers), true);
  assert.equal(source.testMaxLines, support.testMaxLines);
});

test("every maintenance tooling root has production and test budgets", () => {
  for (const id of ["repository-tooling", "frontend-tooling", "mcp-tooling"]) {
    for (const field of [
      "productionMaxLines",
      "testMaxLines",
      "testPackageMaxLines",
    ]) {
      assert.ok(budgets.get(id)?.[field], `${id} ${field}`);
    }
  }
});

test("frontend runtime, browser, and public roots all have source budgets", () => {
  for (const id of [
    "frontend",
    "frontend-e2e",
    "frontend-e2e-real",
    "frontend-public",
  ]) {
    assert.ok(budgets.has(id), `${id} source budget`);
  }
  assert.equal(budgets.get("frontend-e2e").classifier, "all");
  assert.equal(budgets.get("frontend-e2e-real").classifier, "all");
  assert.ok(budgets.get("frontend-public").productionMaxLines);
});

test("gateway behavior owners have explicit backend coverage floors", () => {
  for (const packagePath of [
    "internal/gatewayaccess",
    "internal/gatewayaccess/httpowner",
    "internal/gatewayconnectorapi",
    "internal/gatewayconnectormanagement",
    "internal/gatewayinfrastructure",
    "internal/gatewayvault",
    "internal/gatewayworkspace",
  ]) {
    assert.ok(
      policy.backendCoverageFloors[packagePath] > 0,
      `${packagePath} coverage floor`,
    );
  }
});

test("rejects unknown classifiers instead of silently weakening policy", () => {
  assert.throws(
    () => isTestSource("unknown", "/tmp/service.test.js", markers),
    /unknown maintenance source classifier/,
  );
});
