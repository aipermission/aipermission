import assert from "node:assert/strict";
import test from "node:test";

import {
  coverageFloors,
  mergeChangedCoverageBaseline,
  mergeCoverageMetrics,
  ratchetedMetrics,
  validateCoverageBaseline,
} from "./coverage-ratchet.mjs";

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

test("baseline updates preserve stronger accepted coverage", () => {
  assert.deepEqual(
    mergeCoverageMetrics(
      { statements: 80, branches: 62, functions: 71, lines: 79 },
      { statements: 79, branches: 64, functions: 70, lines: 81 },
    ),
    { statements: 80, branches: 64, functions: 71, lines: 81 },
  );
});

test("baseline updates merge only changed behavior owners", () => {
  const previous = {
    stable: { statements: 80, branches: 70, functions: 80, lines: 80 },
    changed: { statements: 50, branches: 40, functions: 50, lines: 50 },
  };
  const measured = {
    stable: { statements: 90, branches: 90, functions: 90, lines: 90 },
    changed: { statements: 55, branches: 42, functions: 51, lines: 56 },
  };
  expectBaseline(mergeChangedCoverageBaseline(["stable", "changed"], ["changed"], previous, measured), previous.stable, measured.changed);
});

function expectBaseline(actual, stable, changed) {
  assert.deepEqual(actual, { stable, changed });
}
