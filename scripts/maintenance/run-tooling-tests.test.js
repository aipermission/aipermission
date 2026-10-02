const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const { temporaryRoot } = require("./test-fixtures");

const { discoverTestFiles, resolveTestRoots, run, verifyTestInventory } = require("../run-tooling-tests");

test("tooling test discovery is deterministic and includes nested files", (t) => {
  const root = temporaryRoot(t, {
    "first/z.test.js": "",
    "first/helper.js": "",
    "first/nested/hidden.test.js": "",
    "second/a.spec.mjs": "",
    "second/also.test.cjs": "",
  });
  const first = path.join(root, "first");
  const second = path.join(root, "second");

  assert.deepEqual(discoverTestFiles([second, first]), [
    path.join(first, "nested", "hidden.test.js"),
    path.join(first, "z.test.js"),
    path.join(second, "a.spec.mjs"),
    path.join(second, "also.test.cjs"),
  ]);
  assert.throws(() => discoverTestFiles([temporaryRoot(t)]), /no tooling tests discovered/);
});

test("tooling test inventory rejects missing and unregistered files", (t) => {
  const root = temporaryRoot(t, {
    "scripts/ci/first.test.js": "",
    "scripts/ci/nested/second.test.js": "",
  });
  const owner = path.join(root, "scripts", "ci");
  const first = path.join(owner, "first.test.js");
  const nested = path.join(owner, "nested", "second.test.js");

  const inventory = ["scripts/ci/first.test.js", "scripts/ci/nested/second.test.js"];
  for (const [files, expected, error] of [
    [[first], inventory, /missing: scripts\/ci\/nested\/second\.test\.js/],
    [[first, nested], inventory.slice(0, 1), /unregistered: scripts\/ci\/nested\/second\.test\.js/],
  ]) {
    assert.throws(() => verifyTestInventory(files, [owner], expected, root), error);
  }
});

test("tooling runner discovers tests outside configured roots and fails closed", (t) => {
  const root = temporaryRoot(t, {
    "scripts/ci/first.test.js": "",
    "scripts/unwired/hidden.spec.mjs": "",
  });
  const owner = path.join(root, "scripts", "ci");
  const outside = path.join(root, "scripts", "unwired", "hidden.spec.mjs");

  const options = {
    repositoryRoot: root,
    configured: [owner],
    expected: ["scripts/ci/first.test.js"],
    spawnSync: () => assert.fail("runner spawned an unregistered test"),
  };
  assert.throws(() => run([owner], options), /unregistered: scripts\/unwired\/hidden\.spec\.mjs/);
  fs.rmSync(outside);
  fs.symlinkSync(path.join(owner, "first.test.js"), outside);
  assert.throws(() => run([owner], options), /tooling test symlink is not allowed/);
});
test("tooling runner validates the full inventory and executes only the requested roots", (t) => {
  const owners = ["ci", "maintenance", "release"];
  const root = temporaryRoot(t, Object.fromEntries(owners.map((owner) => [`scripts/${owner}/owner.test.js`, ""])));
  const configured = owners.map((owner) => path.join(root, "scripts", owner));
  const files = configured.map((owner) => path.join(owner, "owner.test.js"));
  let invocation;
  const status = run([configured[0]], {
    repositoryRoot: root,
    configured,
    expected: files.map((file) => path.relative(root, file).replaceAll(path.sep, "/")),
    spawnSync: (command, args) => ((invocation = { command, args }), { status: 0 }),
  });
  assert.equal(status, 0);
  assert.deepEqual(invocation, {
    command: process.execPath,
    args: ["--test", files[0]],
  });
  assert.throws(() => resolveTestRoots([path.join(root, "scripts", "unregistered")], root, configured), /unregistered tooling test root/);
});
