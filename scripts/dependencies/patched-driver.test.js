const { test } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { checkPatchedDriver, metadataPath } = require("./patched-driver");
const { goRecords, parseGoSum } = require("../dependency-license-inputs");
const repository = path.resolve(__dirname, "../..");

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "patched-driver-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.cpSync(path.join(repository, "third_party"), path.join(root, "third_party"), { recursive: true });
  fs.mkdirSync(path.join(root, "backend"));
  fs.copyFileSync(path.join(repository, "backend/go.sum"), path.join(root, "backend/go.sum"));
  return root;
}

test("only the verified driver source and pinned local replacement enter the license inventory", (t) => {
  const root = fixture(t);
  const patch = checkPatchedDriver(root);
  const sums = parseGoSum(fs.readFileSync(path.join(root, "backend/go.sum"), "utf8"));
  const mod = {
    Module: { Path: "example.test/application" },
    Require: [{ Path: patch.module, Version: patch.version }],
    Replace: [{ Old: { Path: patch.module }, New: { Path: patch.path } }],
  };
  assert.equal(
    goRecords(sums, mod, [patch]).find((entry) => entry.module === patch.module && entry.version === patch.version)
      .local_patch.sha256,
    patch.sha256,
  );
  assert.throws(() => goRecords(sums, mod), /Unsupported go.mod Replace/);
  mod.Replace[0].New.Path = "../unreviewed-driver";
  assert.throws(() => goRecords(sums, mod, [patch]), /Unsupported go.mod Replace/);
});

for (const kind of ["modified", "added", "symlink", "expanded-patch", "upstream-checksum", "patch-drift"]) {
  test(`driver integrity rejects ${kind} source drift`, (t) => {
    const root = fixture(t);
    const driver = path.join(root, "third_party/go-mssqldb");
    const metadata = JSON.parse(fs.readFileSync(path.join(root, metadataPath), "utf8"));
    if (kind === "modified") fs.appendFileSync(path.join(driver, "mssql.go"), "\n// drift\n");
    if (kind === "added") fs.writeFileSync(path.join(driver, "unreviewed.go"), "package mssql\n");
    if (kind === "symlink") fs.symlinkSync(path.join(driver, "mssql.go"), path.join(driver, "alias.go"));
    if (kind === "expanded-patch") metadata.patched_files["LICENSE.txt"] = metadata.files["LICENSE.txt"];
    if (kind === "upstream-checksum") metadata.module_sum = "h1:unreviewed";
    if (kind === "patch-drift") fs.appendFileSync(path.join(root, "third_party/go-mssqldb-close.patch"), "\n");
    fs.writeFileSync(path.join(root, metadataPath), JSON.stringify(metadata));
    assert.throws(() => checkPatchedDriver(root), /driver|Driver/);
  });
}
