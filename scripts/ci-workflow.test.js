const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");

const root = path.resolve(__dirname, "..");
const workflow = fs.readFileSync(
  path.join(root, ".github/workflows/ci.yml"),
  "utf8",
);
const makefile = fs.readFileSync(path.join(root, "Makefile"), "utf8");

test("CI verifies both generated connector catalogs", () => {
  assert.match(workflow, /node scripts\/connector-catalog\.js --check/);
  assert.match(workflow, /node scripts\/mcp-client-catalog\.mjs --check/);
});

test("CI uses the fail-closed Windows ACL suite", () => {
  assert.match(workflow, /run: npm run test:windows-acl/);
  assert.doesNotMatch(
    workflow,
    /node --test --test-name-pattern=.*Windows ACL/,
  );
});

test("the recovery drill uses its exact manifest runner", () => {
  assert.match(
    makefile,
    /recovery-drill:\n\tsh scripts\/run-recovery-drill\.sh/,
  );
  assert.doesNotMatch(makefile, /internal\/migration -run RecoveryDrill/);
});

test("frontend ratchets derive trusted bases from the GitHub event", () => {
  for (const variable of [
    "FRONTEND_COVERAGE_BASE",
    "FRONTEND_DUPLICATION_BASE",
    "PLAYWRIGHT_GATE_BASE",
  ]) {
    assert.doesNotMatch(workflow, new RegExp(`${variable}:`));
  }
});
