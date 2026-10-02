#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { isDeepStrictEqual, parseArgs } = require("node:util");
const {
  boundary,
  cacheIdentity,
  goRecords,
  inventoryPath,
  moduleIdentity,
  nonempty,
  object,
  readInputs,
} = require("./dependency-license-inputs");

function same(actual, expected, label) {
  if (!isDeepStrictEqual(actual, expected)) throw new Error(`${label} differs from reviewed inventory; regenerate offline and review`);
}

function check(root = path.resolve(__dirname, ".."), file = inventoryPath) {
  const inventory = object(JSON.parse(fs.readFileSync(path.resolve(root, file), "utf8")), "dependency license inventory");
  if (inventory.schema_version !== 1) throw new Error("Unsupported dependency license inventory schema");
  same(inventory.boundary, boundary, "Inventory boundary");
  const inputs = readInputs(root);
  same(inventory.sources, inputs.sources, "Dependency source identity");
  same(inventory.npm, inputs.npm, "npm dependency metadata");
  same(inventory.native, inputs.native, "Native dependency reference");
  const records = goRecords(inputs.sums, inventory.go_mod);
  if (!Array.isArray(inventory.go)) throw new Error("Missing Go inventory");
  same(
    inventory.go.map(({ evidence, ...record }) => record),
    records,
    "Go dependency metadata",
  );
  const gaps = [];
  for (const record of inventory.go) {
    const evidence = object(record.evidence, "Go evidence");
    if (!Array.isArray(evidence.files)) throw new Error("Missing Go evidence files");
    if (evidence.status === "owner-review-required") {
      if (evidence.files.length || !["module-not-in-local-cache", "no-named-license-evidence"].includes(evidence.reason))
        throw new Error("Invalid Go evidence gap");
      gaps.push({ identity: moduleIdentity(record.module, record.version), reason: evidence.reason });
    } else if (evidence.status === "files-recorded" && evidence.files.length && !evidence.reason) {
      const prefix = `${cacheIdentity(record.module, record.version)}/`;
      const paths = new Set();
      for (const file of evidence.files) {
        if (
          typeof file.path !== "string" ||
          !file.path.startsWith(prefix) ||
          file.path.includes("\\") ||
          file.path.split("/").some((part) => !part || part === "." || part === "..") ||
          paths.has(file.path) ||
          !/^[a-f0-9]{64}$/.test(file.sha256) ||
          !Number.isSafeInteger(file.bytes) ||
          file.bytes < 1
        )
          throw new Error("Invalid Go evidence file identity");
        paths.add(file.path);
      }
    } else throw new Error("Invalid Go evidence status");
  }
  const review = object(inventory.review, "metadata review");
  if (review.status !== "metadata-reviewed") throw new Error("Dependency metadata owner review is pending");
  nonempty(review.reviewer, "metadata reviewer");
  same(review.scope, "Metadata and evidence gaps only; no legal or SPDX compatibility assertion", "Review scope");
  if (!Array.isArray(review.missing_go_evidence)) throw new Error("Missing Go evidence owner review");
  same(
    review.missing_go_evidence.map(({ disposition, note, ...gap }) => gap),
    gaps,
    "Go evidence gaps",
  );
  for (const gap of review.missing_go_evidence) {
    if (gap.disposition !== "acknowledged-owner-review-required") throw new Error("Unacknowledged Go evidence gap");
    nonempty(gap.note, `Go owner review note: ${gap.identity}`);
  }
  return { npm: inventory.npm.length, go: records.length, gaps: gaps.length };
}

function main() {
  const { values } = parseArgs({
    options: {
      root: { type: "string", default: path.resolve(__dirname, "..") },
      inventory: { type: "string", default: inventoryPath },
    },
  });
  const result = check(path.resolve(values.root), values.inventory);
  console.log(
    `Dependency license metadata passed: ${result.npm} npm entries, ${result.go} Go module versions, ${result.gaps} acknowledged owner-review gaps (not legal compliance).`,
  );
}

if (require.main === module) {
  try {
    main();
  } catch (error) {
    console.error(`Dependency license inventory check failed: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { check };
