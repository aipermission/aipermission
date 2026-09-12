const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const policy = require("../maintenance-policy.json");
const { testPackageDirectory, validatePolicy, walk } = require("./maintenance-budget-check");

const copyPolicy = () => structuredClone(policy);

test("rejects disabled budgets and broad test markers", () => {
  const candidate = copyPolicy();
  candidate.sourceBudgets[0].productionMaxLines = 0;
  candidate.backendFanout.familyOwnerMax = 0;
  candidate.frontendArchitecture.testModuleMarkers.push(".jsx");
  const failures = validatePolicy(candidate, []);
  assert.ok(failures.some((failure) => failure.includes("positive integer")));
  assert.ok(failures.some((failure) => failure.includes("markers must be exactly")));
});

test("keeps nested javascript tests in their declared owner package", () => {
  const budget = policy.sourceBudgets.find((item) => item.id === "mcp-test");
  const root = path.resolve(__dirname, "..");
  const owner = (file) => testPackageDirectory(budget, path.join(root, `packages/mcp/test/transport/${file}`));
  assert.equal(owner("http/first.test.js"), owner("ws/second.test.js"));
  assert.equal(owner("http/first.test.js"), path.join(root, "packages/mcp/test/transport"));
});

test("source walking excludes dependency trees at every depth", (t) => {
  const directory = fs.mkdtempSync(path.join(os.tmpdir(), "maintenance-walk-"));
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  fs.mkdirSync(path.join(directory, "nested", "node_modules"), { recursive: true });
  fs.writeFileSync(path.join(directory, "source.js"), "source");
  fs.writeFileSync(path.join(directory, "nested", "node_modules", "dependency.js"), "dependency");
  assert.deepEqual(walk(directory), [path.join(directory, "source.js")]);
});

test("rejects every internal API budget exception form", () => {
  const mutations = [
    (value) => { value.sourceOverrides["backend/internal/api/routes.go"] = 100; },
    (value) => { value.goFunction.overrides["backend/internal/api/routes.go:registerRoutes"] = { lines: 100, complexity: 20 }; },
    (value) => { value.backendFanout.overrides["github.com/aipermission/aipermission/backend/internal/api"] = 10; },
  ];
  for (const mutate of mutations) {
    const candidate = copyPolicy();
    mutate(candidate);
    const failures = validatePolicy(candidate, []);
    assert.ok(failures.some((failure) => failure.includes("internal/api must satisfy shared budgets without exceptions")), failures);
  }
});
