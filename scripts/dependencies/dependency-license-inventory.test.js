const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const { cacheIdentity, goRecords, inventoryPath, npmRecords, parseGoSum, readInputs, sha256 } = require("../dependency-license-inputs");
const { collectEvidence, generate } = require("../dependency-license-generate");
const { check } = require("../dependency-license-check");

const root = path.resolve(__dirname, "../..");
const temporary = fs.mkdtempSync(path.join(os.tmpdir(), "dependency-license-"));
const fixture = path.join(temporary, "repository");
const cache = path.join(temporary, "cache");
const hash = `h1:${Buffer.alloc(32, 1).toString("base64")}`;
const mod = {
  Module: { Path: "example.org/application" },
  Go: "1.22.0",
  Require: [{ Path: "example.org/Upper/v2", Version: "v2.0.0" }],
  Replace: null,
  Exclude: null,
  Retract: null,
};
function write(file, value) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, typeof value === "string" ? value : `${JSON.stringify(value, null, 2)}\n`);
}
function input(file, value) {
  write(path.join(fixture, file), value);
}
function read(file) {
  return JSON.parse(fs.readFileSync(path.join(fixture, file), "utf8"));
}
function reviewed(candidate) {
  const result = structuredClone(candidate);
  result.review.status = "metadata-reviewed";
  result.review.reviewer = "fixture owner";
  for (const gap of result.review.missing_go_evidence) {
    gap.disposition = "acknowledged-owner-review-required";
    gap.note = "Historical checksum-only artifact is absent; owner follow-up required";
  }
  return result;
}
function cli(name, args = [], options = {}) {
  const result = spawnSync(process.execPath, [path.join(root, "scripts", name), ...args], {
    encoding: "utf8",
    timeout: 10_000,
    ...options,
  });
  assert.ifError(result.error);
  return result;
}
function lock(name) {
  return {
    name,
    lockfileVersion: 3,
    packages: {
      "": { name, version: "1.0.0", license: "AGPL-3.0-only" },
      "node_modules/alias": {
        name: "real-package",
        version: "2.0.0",
        license: "(MIT OR Apache-2.0)",
        integrity: "sha512-example",
        resolved: "https://invalid.example/package.tgz",
        optional: true,
        os: ["linux"],
      },
      "node_modules/parent/node_modules/alias": { version: "1.0.0", license: "SEE LICENSE IN LICENSE.txt", dev: true },
    },
  };
}

try {
  fs.mkdirSync(cache, { recursive: true });
  for (const name of ["frontend", "packages/mcp", "scripts"]) {
    input(`${name}/package.json`, { name, version: "1.0.0", license: "AGPL-3.0-only" });
    input(`${name}/package-lock.json`, lock(name));
  }
  input("backend/go.mod", 'module "example.org/application"\n\ngo 1.22.0\n\nrequire "example.org/Upper/v2" v2.0.0\n');
  input(
    "backend/go.sum",
    `example.org/Upper/v2 v2.0.0 ${hash}\nexample.org/Upper/v2 v2.0.0/go.mod ${hash}\nexample.org/history v1.0.0/go.mod ${hash}\n`,
  );
  input("docs/security/native-dependencies.json", { schema_version: 1, sqlcipher: { reference: "fixture" } });
  const evidenceRoot = path.join(cache, cacheIdentity("example.org/Upper/v2", "v2.0.0"));
  write(path.join(evidenceRoot, "LICENSE"), "Fixture license text, not a legal determination\n");
  write(path.join(evidenceRoot, "third_party", "LICENSE-MIT"), "Distinct nested evidence\n");
  write(path.join(evidenceRoot, "NOTICE.md"), "Notice evidence\n");
  write(path.join(evidenceRoot, "notice_response.go"), "Not license evidence\n");
  write(path.join(evidenceRoot, "LICENSE.txt"), "");

  // Real Go parsing handles quoted paths; no module graph or dependency installation.
  const candidate = generate(fixture, cache);
  assert.equal(candidate.go_mod.Require[0].Path, mod.Require[0].Path);
  assert.deepEqual(generate(fixture, cache), candidate, "offline generation must be reproducible");
  assert.equal(candidate.npm.length, 9);
  assert.equal(candidate.npm.find((item) => item.location === "node_modules/alias").name, "real-package");
  assert.equal(candidate.go[0].evidence.files.length, 3);
  assert.equal(candidate.go[0].evidence.files[0].sha256, sha256(fs.readFileSync(path.join(evidenceRoot, "LICENSE"))));
  assert.equal(candidate.go[1].requirement, "sum-only");
  assert.equal(candidate.go[1].evidence.status, "owner-review-required");
  assert.equal(candidate.review.status, "pending-owner-review");
  input(inventoryPath, candidate);
  assert.throws(() => check(fixture), /review is pending/);
  const baseline = reviewed(candidate);
  input(inventoryPath, baseline);
  assert.deepEqual(check(fixture), { npm: 9, go: 2, gaps: 1 });

  const result = cli("dependency-license-check.js", ["--root", fixture], {
    env: { ...process.env, PATH: "", GOMODCACHE: path.join(temporary, "absent-cache") },
  });
  assert.equal(result.status, 0, result.stderr);
  fs.rmSync(cache, { recursive: true });
  assert.deepEqual(check(fixture), { npm: 9, go: 2, gaps: 1 }, "checking must never consult installed evidence");

  // Added, changed and deleted input metadata all fail, including non-license fields.
  for (const directory of ["frontend", "packages/mcp", "scripts"]) {
    const file = `${directory}/package-lock.json`;
    const original = read(file);
    for (const mutate of [
      (value) => {
        value.packages["node_modules/new"] = { version: "1.0.0", license: "MIT" };
      },
      (value) => {
        delete value.packages["node_modules/alias"];
      },
      (value) => {
        value.packages["node_modules/alias"].license = "BSD-3-Clause";
      },
      (value) => {
        delete value.packages["node_modules/alias"].license;
      },
      (value) => {
        value.packages["node_modules/alias"].version = "2.0.1";
      },
      (value) => {
        value.packages["node_modules/alias"].integrity = "sha512-changed";
      },
      (value) => {
        value.packages["node_modules/alias"].os = ["win32"];
      },
    ]) {
      const changed = structuredClone(original);
      mutate(changed);
      input(file, changed);
      assert.throws(() => check(fixture), /differs|Missing npm license/);
      input(file, original);
    }
  }
  for (const source of baseline.sources) {
    const file = path.join(fixture, source.path);
    const original = fs.readFileSync(file);
    fs.appendFileSync(file, "\n");
    assert.throws(() => check(fixture), /Dependency source identity/);
    fs.writeFileSync(file, original);
    fs.unlinkSync(file);
    assert.throws(() => check(fixture), /ENOENT/);
    fs.writeFileSync(file, original);
  }
  const sums = fs.readFileSync(path.join(fixture, "backend/go.sum"), "utf8");
  for (const changed of [
    sums + `example.org/new v1.0.0/go.mod ${hash}\n`,
    sums.split("\n").slice(1).join("\n"),
    sums.replace(hash, `h1:${Buffer.alloc(32, 2).toString("base64")}`),
  ]) {
    input("backend/go.sum", changed);
    assert.throws(() => check(fixture), /Dependency source identity/);
  }
  input("backend/go.sum", sums);

  for (const mutate of [
    (value) => {
      value.schema_version = 2;
    },
    (value) => {
      value.boundary.npm = "All SPDX expressions are compatible";
    },
    (value) => {
      value.npm.pop();
    },
    (value) => {
      value.go.pop();
    },
    (value) => {
      value.go.push(value.go[0]);
    },
    (value) => {
      value.go[0].evidence = { status: "files-recorded", files: [] };
    },
    (value) => {
      value.go[0].evidence.files[0].path = "example.org/!upper/v2@v2.0.0/../LICENSE";
    },
    (value) => {
      value.go[0].evidence.files[0].sha256 = "not-a-hash";
    },
    (value) => {
      value.go[0].evidence.files[0].bytes = 0;
    },
    (value) => {
      value.go[0].evidence.files.push(value.go[0].evidence.files[0]);
    },
    (value) => {
      value.go[1].evidence.reason = "invented-license";
    },
    (value) => {
      value.review.reviewer = null;
    },
    (value) => {
      value.review.missing_go_evidence = [];
    },
    (value) => {
      value.review.missing_go_evidence[0].note = "";
    },
    (value) => {
      value.review.missing_go_evidence[0].disposition = "pending-owner-review";
    },
  ]) {
    const changed = structuredClone(baseline);
    mutate(changed);
    input(inventoryPath, changed);
    assert.throws(() => check(fixture));
  }
  input(inventoryPath, "not-json");
  assert.throws(() => check(fixture), SyntaxError);
  input(inventoryPath, baseline);
  assert.equal(cli("dependency-license-check.js", ["--root", fixture, "--unknown"]).status, 1);

  for (const source of [
    "",
    "invalid",
    `example.org/a v1.0.0 bad\n`,
    `../escape v1.0.0 ${hash}\n`,
    `example.org/a v1.0.0 ${hash}\nexample.org/a v1.0.0 ${hash}\n`,
  ])
    assert.throws(() => parseGoSum(source));
  for (const broken of [
    null,
    [],
    { lockfileVersion: 2 },
    { lockfileVersion: 3, packages: {} },
    { ...lock("fixture"), packages: { "": { version: "1.0.0", license: 42 } } },
  ])
    assert.throws(() => npmRecords(broken, "fixture"));
  for (const field of ["Replace", "Exclude", "Retract"])
    assert.throws(
      () =>
        goRecords(
          candidate.go.map(({ evidence, requirement, ...sum }) => sum),
          { ...mod, [field]: [{}] },
        ),
      /Unsupported/,
    );
  assert.throws(() => goRecords(parseGoSum(`example.org/Upper/v2 v2.0.0/go.mod ${hash}\n`), mod), /Missing required Go checksums/);
  assert.equal(goRecords(parseGoSum(sums), { ...mod, Require: [{ ...mod.Require[0], Indirect: true }] })[0].requirement, "indirect");
  for (const broken of [
    { ...mod, Replace: {} },
    { ...mod, Require: [{ ...mod.Require[0], Indirect: "true" }] },
    { ...mod, Require: [...mod.Require, ...mod.Require] },
  ])
    assert.throws(() => goRecords(parseGoSum(sums), broken), /Unsupported|Invalid|Duplicate/);
  for (const field of ["resolved", "integrity"]) {
    const broken = lock("fixture");
    broken.packages["node_modules/alias"][field] = {};
    assert.throws(() => npmRecords(broken, "fixture"), /Missing npm/);
  }

  fs.mkdirSync(cache);
  assert.equal(collectEvidence(cache, candidate.go[0]).reason, "module-not-in-local-cache");
  fs.mkdirSync(evidenceRoot, { recursive: true });
  assert.equal(collectEvidence(cache, candidate.go[0]).reason, "no-named-license-evidence");
  fs.symlinkSync("/missing", path.join(evidenceRoot, "LICENSE"));
  assert.throws(() => collectEvidence(cache, candidate.go[0]), /symlink/);
  fs.unlinkSync(path.join(evidenceRoot, "LICENSE"));
  write(path.join(evidenceRoot, "LICENSE"), "Changed evidence\n");
  assert.notDeepEqual(generate(fixture, cache).go[0].evidence, candidate.go[0].evidence);

  const output = path.join(temporary, "candidate.json");
  assert.equal(cli("dependency-license-generate.js", ["--root", fixture, "--go-mod-cache", cache, "--output", output]).status, 0);
  assert.equal(JSON.parse(fs.readFileSync(output)).review.status, "pending-owner-review");
  fs.unlinkSync(output);
  for (const args of [[], ["--go-mod-cache", "/missing"], ["--go-mod-cache", cache, "--go-binary", "/missing"]]) {
    const failure = cli("dependency-license-generate.js", ["--root", fixture, "--output", output, ...args]);
    assert.equal(failure.status, 1, failure.stdout);
    assert.equal(fs.existsSync(output), false, "failed generation must not publish output");
  }

  const fakeGo = path.join(temporary, "offline-go");
  write(
    fakeGo,
    `#!${process.execPath}
const assert = require("node:assert/strict");
assert.deepEqual(process.argv.slice(2), ["mod", "edit", "-json", ${JSON.stringify(path.join(fixture, "backend/go.mod"))}]);
for (const [name, value] of Object.entries({GOPROXY:"off", GOSUMDB:"off", GOTOOLCHAIN:"local", GOWORK:"off", GOFLAGS:""})) assert.equal(process.env[name], value);
process.stdout.write(process.env.LICENSE_TEST_GO_RESULT);
`,
  );
  fs.chmodSync(fakeGo, 0o700);
  for (const response of [JSON.stringify(mod), "not-json", "null", JSON.stringify({ ...mod, Replace: [{}] })]) {
    const parsed = cli("dependency-license-generate.js", ["--root", fixture, "--go-mod-cache", cache, "--go-binary", fakeGo], {
      env: { ...process.env, GOPROXY: "https://invalid.example", GOTOOLCHAIN: "auto", LICENSE_TEST_GO_RESULT: response },
    });
    assert.equal(parsed.status, response === JSON.stringify(mod) ? 0 : 1, parsed.stderr);
    if (!parsed.status) assert.equal(JSON.parse(parsed.stdout).review.status, "pending-owner-review");
    else assert.equal(parsed.stdout, "", "malformed parser output must not emit a candidate");
  }

  // The actual hygiene entry point must reject license drift with native pins intact.
  const integration = path.join(temporary, "hygiene");
  const committed = JSON.parse(fs.readFileSync(path.join(root, inventoryPath)));
  for (const file of [
    ...committed.sources.map((source) => source.path),
    inventoryPath,
    "backend/internal/db/db.go",
    "backend/Dockerfile",
    "scripts/native-dependency-check.js",
    "scripts/dependency-license-check.js",
    "scripts/dependency-license-inputs.js",
  ]) {
    const destination = path.join(integration, file);
    fs.mkdirSync(path.dirname(destination), { recursive: true });
    fs.copyFileSync(path.join(root, file), destination);
  }
  function hygiene() {
    return spawnSync(process.execPath, [path.join(integration, "scripts/native-dependency-check.js")], {
      encoding: "utf8",
      timeout: 10_000,
      env: { ...process.env, PATH: "" },
    });
  }
  const passed = hygiene();
  assert.ifError(passed.error);
  assert.equal(passed.status, 0, passed.stderr);
  const driftFile = path.join(integration, "frontend/package-lock.json");
  const drift = JSON.parse(fs.readFileSync(driftFile));
  drift.packages["node_modules/@eslint/js"].license = "unreviewed-declaration";
  write(driftFile, drift);
  const failed = hygiene();
  assert.ifError(failed.error);
  assert.equal(failed.status, 1);
  assert.match(failed.stderr, /Dependency license metadata: Dependency source identity/);
  console.log("Dependency license inventory fixtures passed.");
} finally {
  fs.rmSync(temporary, { recursive: true, force: true });
}
