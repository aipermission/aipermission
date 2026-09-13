const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");

const { discoverTestFiles } = require("../run-tooling-tests");

test("tooling test discovery is deterministic and ignores nested files", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-tooling-"));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));
  const first = path.join(root, "first");
  const second = path.join(root, "second");
  fs.mkdirSync(path.join(first, "nested"), { recursive: true });
  fs.mkdirSync(second);
  fs.writeFileSync(path.join(first, "z.test.js"), "");
  fs.writeFileSync(path.join(first, "helper.js"), "");
  fs.writeFileSync(path.join(first, "nested", "hidden.test.js"), "");
  fs.writeFileSync(path.join(second, "a.test.js"), "");

  assert.deepEqual(discoverTestFiles([second, first]), [
    path.join(first, "z.test.js"),
    path.join(second, "a.test.js"),
  ]);
});

test("tooling test discovery fails closed for an empty owner", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "aipermission-tooling-"));
  t.after(() => fs.rmSync(root, { force: true, recursive: true }));
  assert.throws(() => discoverTestFiles([root]), /no tooling tests discovered/);
});
