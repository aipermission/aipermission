import assert from "node:assert/strict";
import test from "node:test";

import config, { LexicalSequencer } from "../vitest.changed.config.js";

test("runs changed coverage in a deterministic worker order", () => {
  assert.equal(config.test.fileParallelism, false);
  assert.equal(config.test.maxWorkers, 1);
  assert.equal(config.test.sequence.sequencer, LexicalSequencer);
  assert.equal(config.test.coverage.provider, "istanbul");
  assert.equal(config.test.coverage.all, true);
  assert.equal(config.test.coverage.reportsDirectory, undefined);
});

test("sorts changed coverage files lexically instead of using mutable duration cache", async () => {
  const files = [{ moduleId: "/z.test.jsx" }, { moduleId: "/a.test.jsx" }];
  assert.deepEqual(await LexicalSequencer.prototype.sort.call({}, files), [files[1], files[0]]);
});
