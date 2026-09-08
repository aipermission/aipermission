import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import test from "node:test";

import { createCoverageReportDirectory } from "./coverage-report-directory.mjs";

test("allocates isolated changed coverage report directories", () => {
  const first = createCoverageReportDirectory();
  const second = createCoverageReportDirectory();
  try {
    assert.notEqual(first.path, second.path);
    assert.equal(existsSync(first.path), true);
    assert.equal(existsSync(second.path), true);
  } finally {
    first.cleanup();
    second.cleanup();
  }
  assert.equal(existsSync(first.path), false);
  assert.equal(existsSync(second.path), false);
});
