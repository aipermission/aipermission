import assert from "node:assert/strict";
import test from "node:test";
import { filenameFromKey, joinObjectKey, normalizeObjectKey, parentPrefix, restoreDestinationGuard, safeDownloadName } from "./helpers.js";

test("S3 object-key helpers preserve browser navigation boundaries", () => {
  assert.equal(normalizeObjectKey("  /reports/2026/file.csv  "), "  /reports/2026/file.csv  ");
  assert.equal(joinObjectKey("reports/2026/", "/file.csv"), "reports/2026//file.csv");
  assert.equal(joinObjectKey("a//", " b "), "a// b ");
  assert.equal(parentPrefix("a//"), "a/");
  assert.equal(parentPrefix("/a/"), "/");
  assert.equal(parentPrefix("reports/2026/file.csv"), "reports/2026/");
  assert.equal(parentPrefix("reports/"), "");
});

test("S3 download helpers return bounded local filenames", () => {
  assert.equal(filenameFromKey("reports/2026/file.csv"), "file.csv");
  assert.equal(filenameFromKey(""), "s3-object");
  assert.equal(safeDownloadName("snapshot:latest.aipdb"), "snapshot-latest.aipdb");
});

test("S3 restore guards distinguish current objects from confirmed absence", () => {
  assert.deepEqual(restoreDestinationGuard({ output: { etag: "etag-current" } }), { expected_current_etag: "etag-current" });
  assert.deepEqual(restoreDestinationGuard(null, { actionItem: { output: { code: "not_found" } } }), {
    expected_current_absent: true,
  });
  assert.throws(() => restoreDestinationGuard({ output: {} }), /could not be read/);
  const denied = new Error("permission denied");
  assert.throws(() => restoreDestinationGuard(null, denied), denied);
});
