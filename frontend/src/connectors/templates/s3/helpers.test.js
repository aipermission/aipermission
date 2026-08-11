import assert from "node:assert/strict";
import test from "node:test";
import { filenameFromKey, joinObjectKey, normalizeObjectKey, parentPrefix, safeDownloadName } from "./helpers.js";

test("S3 object-key helpers preserve browser navigation boundaries", () => {
  assert.equal(normalizeObjectKey("  /reports/2026/file.csv  "), "reports/2026/file.csv");
  assert.equal(joinObjectKey("reports/2026/", "/file.csv"), "reports/2026/file.csv");
  assert.equal(parentPrefix("reports/2026/file.csv"), "reports/2026/");
  assert.equal(parentPrefix("reports/"), "");
});

test("S3 download helpers return bounded local filenames", () => {
  assert.equal(filenameFromKey("reports/2026/file.csv"), "file.csv");
  assert.equal(filenameFromKey(""), "s3-object");
  assert.equal(safeDownloadName("snapshot:latest.aipdb"), "snapshot-latest.aipdb");
});
