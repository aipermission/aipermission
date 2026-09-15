const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const root = path.resolve(__dirname, "../..");
const read = (file) => fs.readFileSync(path.join(root, file), "utf8");
const [workflow, makefile] = [
  read(".github/workflows/ci.yml"),
  read("Makefile"),
];
const policy = require("../../maintenance-policy.json");
const rootPackage = require("../../package.json");
test("CI and local release checks preserve every required gate", () => {
  assert.match(workflow, /node scripts\/connector-catalog\.js --check/);
  assert.match(workflow, /node scripts\/mcp-client-catalog\.mjs --check/);
  for (const testRoot of policy.toolingTestRoots)
    assert.match(workflow, new RegExp(`run-tooling-tests\\.js ${testRoot}`));
  assert.match(
    makefile,
    /frontend-shared-duplication:\n\tcd frontend && npm run test:duplication:shared[\s\S]*frontend-types:\n\tcd frontend && npm run test:types[\s\S]*frontend-initial-bundle: frontend-build\n\tcd frontend && npm run test:bundle:initial[\s\S]*release-check:[^\n]*frontend-shared-duplication[^\n]*frontend-types[^\n]*frontend-initial-bundle/,
  );
  assert.ok(
    [
      "node scripts/go-toolchain-check.js",
      "node scripts/verification-policy.js --verify-local-release",
      "node scripts/verification-policy.js --verify-ratchet",
    ].every((command) => rootPackage.scripts.hygiene.includes(command)),
  );
});
test("CI uses the fail-closed Windows ACL suite", () => {
  assert.match(workflow, /run: npm run test:windows-acl/);
  assert.doesNotMatch(workflow, /node --test.+Windows ACL/);
});
test("native Windows runtime evidence stays on a Windows runner", () => {
  assert.match(
    workflow,
    /backend-windows-runtime:[\s\S]*?runs-on: windows-latest[\s\S]*?node \.\.\/scripts\/ci\/windows-runtime-tests\.js/,
  );
});
test("backend CI enforces formatting and bounded fuzz dependencies", () => {
  const backendJob =
    /\n  backend:\n([\s\S]*?)\n  backend-windows-runtime:/.exec(
      workflow,
    )?.[1] || "";
  assert.match(
    backendJob,
    /name: Go format\s+run: ['"]test -z "\$\{MAKEFILES:-\}" && make -f Makefile backend-format-check['"]/,
  );
  assert.match(
    backendJob,
    /name: Generated REST contracts\s+run: ['"]test -z "\$\{MAKEFILES:-\}" && make -f Makefile rest-contract-check['"]/,
  );
  assert.match(
    makefile,
    /backend-format-check:\n\tsh scripts\/go-format-check/,
  );
  assert.match(
    backendJob,
    /setup-node@[a-f0-9]+[\s\S]*?npm ci --prefix scripts --workspaces=false[\s\S]*?make -f Makefile bounded-fuzz/,
  );
});
test("the recovery drill uses its exact manifest runner", () => {
  assert.match(makefile, /recovery-drill:\n\tsh scripts\/run-recovery-drill/);
  assert.doesNotMatch(makefile, /internal\/migration -run RecoveryDrill/);
});
test("frontend ratchets derive trusted bases from the GitHub event", () => {
  for (const variable of [
    "FRONTEND_COVERAGE_BASE",
    "FRONTEND_DUPLICATION_BASE",
    "FRONTEND_TEST_OWNER_BASE",
    "PLAYWRIGHT_GATE_BASE",
  ]) {
    assert.doesNotMatch(workflow, new RegExp(`${variable}:`));
  }
});
