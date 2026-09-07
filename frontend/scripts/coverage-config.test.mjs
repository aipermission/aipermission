import assert from "node:assert/strict";
import test from "node:test";

import config from "../vitest.changed.config.js";

test("runs changed coverage in a deterministic worker order", () => {
  assert.equal(config.test.fileParallelism, false);
  assert.equal(config.test.maxWorkers, 1);
  assert.equal(config.test.coverage.provider, "istanbul");
  assert.equal(config.test.coverage.all, true);
  assert.equal(config.test.coverage.reportsDirectory, undefined);
});
