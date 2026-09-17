const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { verifyNodeToolchain } = require("../node-toolchain-check");

function fixture(t, { runtime = "24.21.0", workflow = "24.21.0", nvm = runtime, docker = runtime } = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "node-toolchain-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, ".github", "workflows"), { recursive: true });
  fs.writeFileSync(path.join(root, ".node-version"), `${runtime}\n`);
  fs.writeFileSync(path.join(root, ".nvmrc"), `${nvm}\n`);
  fs.mkdirSync(path.join(root, "frontend"));
  fs.writeFileSync(
    path.join(root, "frontend", "Dockerfile"),
    `FROM node:${docker}-alpine@sha256:${"b".repeat(64)} AS build\n`,
  );
  fs.writeFileSync(
    path.join(root, "CONTRIBUTING.md"),
    `Use Node.js ${runtime}.\n`,
  );
  fs.writeFileSync(
    path.join(root, ".github", "workflows", "ci.yml"),
    `name: CI\njobs:\n  test:\n    steps:\n      - uses: actions/setup-node@${"a".repeat(40)}\n        with:\n          node-version: ${workflow}\n`,
  );
  return root;
}

test("repository contributor and CI Node.js versions stay exact", () => {
  assert.doesNotThrow(() => verifyNodeToolchain());
});

test("toolchain check parses quoted setup-node steps independent of key order", (t) => {
  const root = fixture(t);
  fs.writeFileSync(
    path.join(root, ".github", "workflows", "ci.yml"),
    `name: CI\njobs:\n  test:\n    steps:\n      - with:\n          node-version: "24.21.0"\n        uses: "actions/setup-node@${"a".repeat(40)}"\n`,
  );
  assert.doesNotThrow(() => verifyNodeToolchain({ root, runtimeVersion: "24.21.0" }));
});

test("toolchain check rejects nvm and frontend builder drift", (t) => {
  assert.throws(
    () => verifyNodeToolchain({ root: fixture(t, { nvm: "24.20.0" }), runtimeVersion: "24.21.0" }),
    /.nvmrc uses Node.js 24\.20\.0/,
  );
  assert.throws(
    () => verifyNodeToolchain({ root: fixture(t, { docker: "24.17.0" }), runtimeVersion: "24.21.0" }),
    /frontend\/Dockerfile uses Node.js 24\.17\.0/,
  );
});

test("toolchain check rejects the active wrong runtime", (t) => {
  const root = fixture(t);
  assert.throws(
    () => verifyNodeToolchain({ root, runtimeVersion: "22.21.0" }),
    /nvm install && nvm use/,
  );
});

test("toolchain check rejects CI version drift", (t) => {
  const root = fixture(t, { workflow: "24" });
  assert.throws(
    () => verifyNodeToolchain({ root, runtimeVersion: "24.21.0" }),
    /expected 24\.21\.0/,
  );
});

test("toolchain check rejects setup-node without an exact version", (t) => {
  const root = fixture(t, { workflow: "" });
  assert.throws(
    () => verifyNodeToolchain({ root, runtimeVersion: "24.21.0" }),
    /without a version/,
  );
});
