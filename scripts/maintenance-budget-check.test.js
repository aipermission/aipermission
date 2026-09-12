const assert = require("node:assert/strict");
const path = require("node:path");
const test = require("node:test");

const policy = require("../maintenance-policy.json");
const {
  testPackageDirectory,
  validatePolicy,
} = require("./maintenance-budget-check");

function copyPolicy() {
  return JSON.parse(JSON.stringify(policy));
}

test("rejects disabled budgets and broad test markers", () => {
  const candidate = copyPolicy();
  candidate.sourceBudgets[0].productionMaxLines = 0;
  candidate.frontendArchitecture.testModuleMarkers.push(".jsx");
  const failures = validatePolicy(candidate, []);
  assert.ok(failures.some((failure) => failure.includes("positive integer")));
  assert.ok(
    failures.some((failure) => failure.includes("markers must be exactly")),
  );
});

test("keeps nested javascript tests in their declared owner package", () => {
  const budget = policy.sourceBudgets.find((item) => item.id === "mcp-test");
  const root = path.resolve(__dirname, "..");
  const first = testPackageDirectory(
    budget,
    path.join(root, "packages/mcp/test/transport/http/first.test.js"),
  );
  const second = testPackageDirectory(
    budget,
    path.join(root, "packages/mcp/test/transport/ws/second.test.js"),
  );
  assert.equal(first, second);
  assert.equal(first, path.join(root, "packages/mcp/test/transport"));
});
