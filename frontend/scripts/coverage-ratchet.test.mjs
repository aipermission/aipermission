import assert from "node:assert/strict";
import test from "node:test";

import { coverageFloors, ratchetedMetrics, validateCoverageBaseline } from "./coverage-ratchet.mjs";

test("new owners must meet the full coverage floor", () => {
  assert.deepEqual(ratchetedMetrics(null), coverageFloors);
});

test("existing debt must improve on every change until it reaches the floor", () => {
  assert.deepEqual(ratchetedMetrics({ statements: 0, branches: 59.5, functions: 70, lines: 74.5 }), {
    statements: 1,
    branches: 60,
    functions: 70,
    lines: 75,
  });
});

test("malformed base metrics fail closed", () => {
  assert.throws(() => ratchetedMetrics({ statements: 10 }), /missing branches/);
});

test("baseline validation rejects hidden and unclassified owners", () => {
  assert.throws(
    () => validateCoverageBaseline({ version: 2, files: { "src/known.js": coverageFloors } }, ["src/known.js", "src/missing.js"]),
    /owner mismatch.*missing: src\/missing.js/,
  );
});
