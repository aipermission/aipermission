const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const {
  discoverTestFiles,
  resolveTestRoots,
  run,
  verifyTestInventory,
} = require("../run-tooling-tests");

function temporaryRoot(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-tooling-"));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));
  return root;
}

test("tooling test discovery is deterministic and includes nested files", (t) => {
  const root = temporaryRoot(t);
  const first = path.join(root, "first");
  const second = path.join(root, "second");
  fs.mkdirSync(path.join(first, "nested"), { recursive: true });
  fs.mkdirSync(second);
  fs.writeFileSync(path.join(first, "z.test.js"), "");
  fs.writeFileSync(path.join(first, "helper.js"), "");
  fs.writeFileSync(path.join(first, "nested", "hidden.test.js"), "");
  fs.writeFileSync(path.join(second, "a.spec.mjs"), "");
  fs.writeFileSync(path.join(second, "also.test.cjs"), "");

  assert.deepEqual(discoverTestFiles([second, first]), [
    path.join(first, "nested", "hidden.test.js"),
    path.join(first, "z.test.js"),
    path.join(second, "a.spec.mjs"),
    path.join(second, "also.test.cjs"),
  ]);
  assert.throws(
    () => discoverTestFiles([temporaryRoot(t)]),
    /no tooling tests discovered/,
  );
});

test("tooling test inventory rejects missing and unregistered files", (t) => {
  const root = temporaryRoot(t);
  const owner = path.join(root, "scripts", "ci");
  fs.mkdirSync(path.join(owner, "nested"), { recursive: true });
  const first = path.join(owner, "first.test.js");
  const nested = path.join(owner, "nested", "second.test.js");
  fs.writeFileSync(first, "");
  fs.writeFileSync(nested, "");

  assert.throws(
    () =>
      verifyTestInventory(
        [first],
        [owner],
        ["scripts/ci/first.test.js", "scripts/ci/nested/second.test.js"],
        root,
      ),
    /missing: scripts\/ci\/nested\/second\.test\.js/,
  );
  assert.throws(
    () =>
      verifyTestInventory(
        [first, nested],
        [owner],
        ["scripts/ci/first.test.js"],
        root,
      ),
    /unregistered: scripts\/ci\/nested\/second\.test\.js/,
  );
});

test("tooling runner discovers tests outside configured roots and fails closed", (t) => {
  const root = temporaryRoot(t);
  const owner = path.join(root, "scripts", "ci");
  const outside = path.join(root, "scripts", "unwired", "hidden.spec.mjs");
  fs.mkdirSync(owner, { recursive: true });
  fs.mkdirSync(path.dirname(outside), { recursive: true });
  fs.writeFileSync(path.join(owner, "first.test.js"), "");
  fs.writeFileSync(outside, "");

  const options = {
    repositoryRoot: root,
    configured: [owner],
    expected: ["scripts/ci/first.test.js"],
    spawnSync: () => assert.fail("runner spawned an unregistered test"),
  };
  assert.throws(
    () => run([owner], options),
    /unregistered: scripts\/unwired\/hidden\.spec\.mjs/,
  );
  fs.rmSync(outside);
  fs.symlinkSync(path.join(owner, "first.test.js"), outside);
  assert.throws(
    () => run([owner], options),
    /tooling test symlink is not allowed/,
  );
});

test("tooling runner expands every registered subset to the full inventory", (t) => {
  const root = temporaryRoot(t);
  const configured = ["ci", "maintenance", "release"].map((owner) =>
    path.join(root, "scripts", owner),
  );
  for (const owner of configured) fs.mkdirSync(owner, { recursive: true });

  assert.deepEqual(
    resolveTestRoots([configured[0]], root, configured),
    configured,
  );
  assert.throws(
    () =>
      resolveTestRoots(
        [path.join(root, "scripts", "unregistered")],
        root,
        configured,
      ),
    /unregistered tooling test root/,
  );
});
