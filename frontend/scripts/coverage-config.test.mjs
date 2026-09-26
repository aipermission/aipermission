import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import test from "node:test";

import config, { LexicalSequencer } from "../vitest.changed.config.js";
import riskConfig from "../vitest.risk.config.js";
import focusedConfig from "../vitest.config.js";
import { riskCoverageTestIncludes } from "../test-suite-manifests.mjs";

test("every focused coverage owner exists after extension migrations", () => {
  for (const owner of focusedConfig.test.coverage.include) {
    assert.ok(existsSync(new URL(`../${owner}`, import.meta.url)), `Missing focused coverage owner: ${owner}`);
  }
  assert.ok(focusedConfig.test.coverage.include.includes("src/components/console/connector-token-permission-panel.tsx"));
});

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

test("keeps migrated connector forms in the risk coverage gate", () => {
  assert.ok(riskConfig.test.coverage.include.includes("src/connectors/templates/_shared/network-transport-fields.{jsx,tsx}"));
  assert.ok(
    riskConfig.test.coverage.include.includes("src/connectors/templates/{docker,kafka,kubernetes,mail,rabbitmq,redis,s3}/form.{jsx,tsx}"),
  );
  assert.ok(riskCoverageTestIncludes.includes("src/connectors/templates/_shared/runtime-scope-forms.component.test.tsx"));
});
