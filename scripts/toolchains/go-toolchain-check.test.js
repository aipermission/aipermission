const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const test = require("node:test");
const { verifyGoToolchain } = require("../go-toolchain-check");

function fixture(t, versions = ["1.26.6", "1.26.6"]) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "go-toolchain-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  fs.mkdirSync(path.join(root, "backend"));
  fs.mkdirSync(path.join(root, ".github", "workflows"), { recursive: true });
  fs.writeFileSync(
    path.join(root, "backend/go.mod"),
    "module fixture\n\ntoolchain go1.26.6\n",
  );
  fs.writeFileSync(
    path.join(root, "backend/Dockerfile"),
    "FROM golang:1.26.6-bookworm AS build\n",
  );
  const jobs = versions
    .map(
      (version, index) =>
        `  job${index}:\n    steps:\n      - with:\n          go-version: ${JSON.stringify(version)}\n        uses: "actions/setup-go@${"a".repeat(40)}"\n`,
    )
    .join("");
  fs.writeFileSync(
    path.join(root, ".github/workflows/ci.yml"),
    `name: CI\njobs:\n${jobs}`,
  );
  return root;
}

test("every repository Go workflow and builder declares the canonical toolchain", () => {
  assert.doesNotThrow(() => verifyGoToolchain());
});

test("Go version verification rejects later job drift, not only the first match", (t) => {
  assert.throws(
    () => verifyGoToolchain({ root: fixture(t, ["1.26.6", "1.25.0"]) }),
    /ci.yml uses Go 1.25.0/,
  );
});

test("Go version verification rejects missing and structured versions", (t) => {
  assert.throws(
    () => verifyGoToolchain({ root: fixture(t, ["1.26.6", ""]) }),
    /without a version/,
  );
  assert.throws(
    () => verifyGoToolchain({ root: fixture(t, ["1.26.6", ["1.26.6"]]) }),
    /must be a string or number/,
  );
});

test("Go version verification includes extra workflows and composite actions", (t) => {
  const root = fixture(t);
  const file = path.join(root, ".github/workflows/extra.yaml");
  fs.writeFileSync(
    file,
    `jobs:\n  extra:\n    steps:\n      - uses: actions/setup-go@${"a".repeat(40)}\n        with:\n          go-version: 1.25.0\n`,
  );
  assert.throws(() => verifyGoToolchain({ root }), /extra.yaml uses Go 1.25.0/);
  fs.unlinkSync(file);
  fs.mkdirSync(path.join(root, ".github/actions/local"), { recursive: true });
  fs.writeFileSync(
    path.join(root, ".github/actions/local/action.yml"),
    `name: fixture\nruns:\n  using: composite\n  steps:\n    - uses: actions/setup-go@${"a".repeat(40)}\n      with:\n        go-version: 1.25.0\n`,
  );
  assert.throws(() => verifyGoToolchain({ root }), /action.yml uses Go 1.25.0/);
});

test("Go version verification rejects a later builder mismatch", (t) => {
  const root = fixture(t);
  fs.appendFileSync(
    path.join(root, "backend/Dockerfile"),
    "FROM golang:1.25.0-alpine AS other\n",
  );
  assert.throws(() => verifyGoToolchain({ root }), /Dockerfile uses Go 1.25.0/);
});

test("Go builder verification includes platform-qualified and bare image tags", (t) => {
  for (const declaration of [
    "FROM --platform=linux/amd64 golang:1.25.0-bookworm AS other",
    "FROM golang:1.25.0 AS other",
  ]) {
    const root = fixture(t);
    fs.appendFileSync(
      path.join(root, "backend/Dockerfile"),
      declaration + "\n",
    );
    assert.throws(
      () => verifyGoToolchain({ root }),
      /Dockerfile uses Go 1.25.0/,
    );
  }
});

test("Go version verification rejects absent setup and ambiguous canonical versions", (t) => {
  const root = fixture(t, []);
  assert.throws(
    () => verifyGoToolchain({ root }),
    /do not declare actions\/setup-go/,
  );
  fs.appendFileSync(path.join(root, "backend/go.mod"), "toolchain go1.25.0\n");
  assert.throws(() => verifyGoToolchain({ root }), /one exact Go toolchain/);
});
