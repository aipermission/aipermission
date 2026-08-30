#!/usr/bin/env node

const assert = require("node:assert/strict");
const { mkdtempSync, mkdirSync, rmSync, writeFileSync } = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const {
  anchorSlugs,
  checkMarkdownFiles,
  githubSlug,
  linkTargets,
} = require("./markdown-link-check.js");

assert.equal(
  githubSlug("Approval & Delivery Safety"),
  "approval--delivery-safety",
);
assert.equal(
  githubSlug("<span><strong>Approval</strong></span> Safety"),
  "approval-safety",
);
assert.equal(githubSlug("<script<script>>unsafe</script> Safe"), "unsafe-safe");
assert.deepEqual(
  [...anchorSlugs("# Repeated\n# Repeated\n")],
  ["repeated", "repeated-1"],
);
assert.deepEqual(
  linkTargets(
    "[Guide](docs/guide.md#setup)\n`[ignored](missing.md)`\n```md\n[ignored](also-missing.md)\n```",
  ),
  ["docs/guide.md#setup"],
);

const fixture = mkdtempSync(path.join(os.tmpdir(), "aipermission-markdown-"));
try {
  mkdirSync(path.join(fixture, "docs"));
  writeFileSync(
    path.join(fixture, "README.md"),
    "[Guide](docs/guide.md#setup)\n",
  );
  writeFileSync(path.join(fixture, "docs/guide.md"), "# Setup\n");
  assert.deepEqual(
    checkMarkdownFiles(["README.md", "docs/guide.md"], fixture),
    [],
  );

  writeFileSync(
    path.join(fixture, "README.md"),
    "[Missing](docs/guide.md#absent)\n",
  );
  assert.match(
    checkMarkdownFiles(["README.md", "docs/guide.md"], fixture)[0],
    /missing anchor/,
  );
} finally {
  rmSync(fixture, { recursive: true, force: true });
}

console.log("Markdown link check tests passed.");
