import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";

import { findChangedOwnerEntries, readBaselineAt, resolveBootstrapRevision, resolveFrontendBase } from "./coverage-git-state.mjs";

const fixtureBaseBranch = "fixture-base";

function git(root, ...args) {
  return execFileSync("git", args, { cwd: root, encoding: "utf8" }).trim();
}

function repositoryFixture() {
  const root = mkdtempSync(join(tmpdir(), "aipermission-coverage-git-"));
  git(root, "init", "-q", "-b", fixtureBaseBranch);
  git(root, "config", "user.email", "fixture@example.invalid");
  git(root, "config", "user.name", "Fixture");
  mkdirSync(join(root, "frontend/src"), { recursive: true });
  writeFileSync(join(root, "frontend/src/existing.js"), "export const existing = true;\n");
  git(root, "add", ".");
  git(root, "commit", "-qm", "base");
  return root;
}

test("discovers tracked and untracked behavior owners before commit", () => {
  const root = repositoryFixture();
  try {
    const base = git(root, "rev-parse", "HEAD");
    writeFileSync(join(root, "frontend/src/existing.js"), "export const existing = false;\n");
    writeFileSync(join(root, "frontend/src/new.mjs"), "export const added = true;\n");
    const entries = findChangedOwnerEntries(root, base, (file) => /\.(?:js|mjs)$/.test(file));
    assert.deepEqual(entries, [
      { file: "src/existing.js", status: "M", untracked: false },
      { file: "src/new.mjs", status: "A", untracked: true },
    ]);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("distinguishes a missing baseline from an invalid base or malformed baseline", () => {
  const root = repositoryFixture();
  try {
    const base = git(root, "rev-parse", "HEAD");
    assert.equal(readBaselineAt(root, base), null);
    assert.throws(() => readBaselineAt(root, "missing-ref"), /not a valid commit/);

    writeFileSync(join(root, "frontend/.changed-coverage-baseline.json"), "{invalid\n");
    git(root, "add", ".");
    git(root, "commit", "-qm", "malformed baseline");
    assert.throws(() => readBaselineAt(root, "HEAD"), /invalid JSON/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("resolves the accepted bootstrap by tree after a rebase rewrites its commit", () => {
  const root = repositoryFixture();
  try {
    const base = git(root, "rev-parse", "HEAD");
    git(root, "switch", "-qc", "original-bootstrap");
    mkdirSync(join(root, "frontend"), { recursive: true });
    writeFileSync(join(root, "frontend/.changed-coverage-baseline.json"), '{"version":2}\n');
    git(root, "add", ".");
    git(root, "commit", "-qm", "original bootstrap");
    const revision = git(root, "rev-parse", "HEAD");
    const tree = git(root, "rev-parse", "HEAD^{tree}");

    git(root, "switch", "-q", fixtureBaseBranch);
    assert.equal(git(root, "rev-parse", "HEAD"), base);
    writeFileSync(join(root, "frontend/.changed-coverage-baseline.json"), '{"version":2}\n');
    git(root, "add", ".");
    git(root, "commit", "-qm", "rebased bootstrap");
    const rewritten = git(root, "rev-parse", "HEAD");

    assert.notEqual(rewritten, revision);
    assert.equal(git(root, "rev-parse", "HEAD^{tree}"), tree);
    assert.equal(resolveBootstrapRevision(root, { revision, tree }), rewritten);
    assert.throws(() => resolveBootstrapRevision(root, { revision, tree: "0".repeat(40) }), /bootstrap tree .* is not reachable/);
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

test("frontend ratchets reject HEAD and non-ancestor configured bases", () => {
  const root = repositoryFixture();
  const localEnvironment = {};
  try {
    const base = git(root, "rev-parse", "HEAD");
    writeFileSync(join(root, "frontend/src/existing.js"), "export const existing = false;\n");
    git(root, "add", ".");
    git(root, "commit", "-qm", "candidate");
    assert.equal(resolveFrontendBase(root, { configured: base, variable: "FRONTEND_TEST_BASE", environment: localEnvironment }), base);
    assert.throws(
      () => resolveFrontendBase(root, { configured: "HEAD", variable: "FRONTEND_TEST_BASE", environment: localEnvironment }),
      /must not resolve to HEAD/,
    );

    git(root, "switch", "-qc", "unrelated", base);
    writeFileSync(join(root, "unrelated.txt"), "unrelated\n");
    git(root, "add", ".");
    git(root, "commit", "-qm", "unrelated");
    const unrelated = git(root, "rev-parse", "HEAD");
    git(root, "switch", "-q", fixtureBaseBranch);
    assert.throws(
      () => resolveFrontendBase(root, { configured: unrelated, variable: "FRONTEND_TEST_BASE", environment: localEnvironment }),
      /is not an ancestor of HEAD/,
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});
