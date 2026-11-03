const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const test = require("node:test");

const start = "<!-- connector-overview:start -->";
const end = "<!-- connector-overview:end -->";

function addConnector(root, kind, label) {
  const directory = path.join(root, "frontend/src/connectors/templates", kind);
  fs.mkdirSync(directory, { recursive: true });
  fs.writeFileSync(
    path.join(directory, "metadata.json"),
    JSON.stringify({
      kind,
      label,
      summary: "Fixture capability.",
      version: "0.1",
    }),
  );
}

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "connector-overview-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, "scripts"));
  fs.mkdirSync(path.join(root, "docs"));
  fs.copyFileSync(
    path.join(__dirname, "../connector-catalog.js"),
    path.join(root, "scripts/connector-catalog.js"),
  );
  fs.writeFileSync(
    path.join(root, "README.md"),
    `Before\n${start}\nold\n${end}\nAfter\n`,
  );
  addConnector(root, "beta", "Beta");
  addConnector(root, "alpha", "Alpha & <DB>");
  return root;
}

function run(root, ...args) {
  return spawnSync(
    process.execPath,
    [path.join(root, "scripts/connector-catalog.js"), ...args],
    {
      cwd: root,
      encoding: "utf8",
    },
  );
}

test("connector generator updates catalog and escaped README names from the same metadata", (t) => {
  const root = fixture(t);
  assert.equal(run(root).status, 0);
  const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");
  assert.match(readme, /<strong>Alpha &amp; &lt;DB&gt;<\/strong>/);
  assert.match(readme, /<strong>Beta<\/strong>/);
  assert.ok(readme.indexOf("Alpha") < readme.indexOf("Beta"));
  assert.ok(readme.startsWith(`Before\n${start}\n`));
  assert.ok(readme.endsWith(`\n${end}\nAfter\n`));
  const catalog = fs.readFileSync(
    path.join(root, "docs/connectors.md"),
    "utf8",
  );
  assert.match(catalog, /Alpha & <DB>/);
  assert.match(catalog, /Beta/);
  assert.equal(run(root, "--check").status, 0);
  assert.equal(run(root).status, 0);
  assert.equal(fs.readFileSync(path.join(root, "README.md"), "utf8"), readme);
});

test("connector check rejects a stale README without modifying either document", (t) => {
  const root = fixture(t);
  assert.equal(run(root).status, 0);
  const catalogPath = path.join(root, "docs/connectors.md");
  const catalog = fs.readFileSync(catalogPath, "utf8");
  const readmePath = path.join(root, "README.md");
  const stale = fs
    .readFileSync(readmePath, "utf8")
    .replace("Alpha", "Outdated");
  fs.writeFileSync(readmePath, stale);
  const checked = run(root, "--check");
  assert.notEqual(checked.status, 0);
  assert.match(checked.stderr, /README\.md is stale/);
  assert.equal(fs.readFileSync(readmePath, "utf8"), stale);
  assert.equal(fs.readFileSync(catalogPath, "utf8"), catalog);
});

test("new connector folders appear in both documents without a hand-maintained name list", (t) => {
  const root = fixture(t);
  assert.equal(run(root).status, 0);
  addConnector(root, "future", "Future Service");
  assert.notEqual(run(root, "--check").status, 0);
  assert.equal(run(root).status, 0);
  for (const file of ["README.md", "docs/connectors.md"]) {
    assert.match(
      fs.readFileSync(path.join(root, file), "utf8"),
      /Future Service/,
    );
  }
  assert.equal(run(root, "--check").status, 0);
});

test("overview uses compact rows and removes connectors deleted from metadata", (t) => {
  const root = fixture(t);
  for (let index = 0; index < 5; index += 1) {
    addConnector(root, `future${index}`, `Gamma ${index}`);
  }
  assert.equal(run(root).status, 0);
  const readme = fs.readFileSync(path.join(root, "README.md"), "utf8");
  assert.equal((readme.match(/<strong>/g) || []).length, 7);
  assert.equal((readme.match(/<br \/>/g) || []).length, 1);
  fs.rmSync(path.join(root, "frontend/src/connectors/templates/future0"), {
    recursive: true,
  });
  assert.notEqual(run(root, "--check").status, 0);
  assert.equal(run(root).status, 0);
  for (const file of ["README.md", "docs/connectors.md"]) {
    assert.doesNotMatch(
      fs.readFileSync(path.join(root, file), "utf8"),
      /Gamma 0/,
    );
  }
  assert.equal(run(root, "--check").status, 0);
});

test("missing, duplicate or reversed overview markers fail before either document is written", (t) => {
  const root = fixture(t);
  const readmePath = path.join(root, "README.md");
  const catalogPath = path.join(root, "docs/connectors.md");
  fs.writeFileSync(catalogPath, "Untouched catalog\n");
  for (const source of [
    "Missing markers\n",
    start,
    end,
    `${end}\n${start}`,
    `${start}\n${start}\n${end}`,
    `${start}\n${end}\n${end}`,
  ]) {
    fs.writeFileSync(readmePath, source);
    assert.notEqual(run(root).status, 0);
    assert.equal(fs.readFileSync(readmePath, "utf8"), source);
    assert.equal(fs.readFileSync(catalogPath, "utf8"), "Untouched catalog\n");
  }
});
