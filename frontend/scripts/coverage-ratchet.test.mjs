import assert from "node:assert/strict";
import test from "node:test";
import { coveragePolicyWeakening, legacyCoveragePolicy, readCoveragePolicy } from "./coverage-policy.mjs";

import {
  coverageFloors,
  mergeChangedCoverageBaseline,
  mergeCoverageMetrics,
  ratchetedMetrics,
  requiredChangedMetrics,
  validateCoverageBaseline,
  validateCoverageBaselineForRun,
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

test("bootstrap applies full floors only to newly added owners", () => {
  const accepted = { statements: 12, branches: 8, functions: 10, lines: 12 };
  assert.deepEqual(requiredChangedMetrics({ baseBaselineAvailable: false, previous: null, added: true, accepted }), coverageFloors);
  assert.deepEqual(requiredChangedMetrics({ baseBaselineAvailable: false, previous: null, added: false, accepted }), accepted);
});

test("an established base baseline ratchets missing owners to full floors", () => {
  assert.deepEqual(
    requiredChangedMetrics({ baseBaselineAvailable: true, previous: null, added: false, accepted: coverageFloors }),
    coverageFloors,
  );
});

test("baseline validation rejects hidden and unclassified owners", () => {
  assert.throws(
    () =>
      validateCoverageBaseline({ version: 2, floors: coverageFloors, files: { "src/known.js": coverageFloors } }, [
        "src/known.js",
        "src/missing.js",
      ]),
    /owner mismatch.*missing: src\/missing.js/,
  );
});

test("baseline update mode accepts a stale owner inventory before remeasuring it", () => {
  const baseline = { version: 2, floors: coverageFloors, files: { "src/known.js": coverageFloors } };
  const owners = ["src/known.js", "src/new.js"];

  assert.throws(() => validateCoverageBaselineForRun(baseline, owners, false), /owner mismatch.*missing: src\/new.js/);
  assert.doesNotThrow(() => validateCoverageBaselineForRun(baseline, owners, true));
});

test("baseline validation rejects missing or weakened floor declarations", () => {
  assert.throws(
    () => validateCoverageBaseline({ version: 2, files: { "src/known.js": coverageFloors } }, ["src/known.js"]),
    /floor mismatch/,
  );
  assert.throws(
    () =>
      validateCoverageBaseline({ version: 2, floors: { ...coverageFloors, branches: 0 }, files: { "src/known.js": coverageFloors } }, [
        "src/known.js",
      ]),
    /floor mismatch for branches/,
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

test("baseline updates cannot undercut the required ratchet", () => {
  const previous = { owner: { statements: 50, branches: 40, functions: 50, lines: 50 } };
  const measured = { owner: { statements: 50.5, branches: 39, functions: 52, lines: 50.5 } };
  const required = { owner: { statements: 51, branches: 41, functions: 51, lines: 51 } };

  assert.deepEqual(mergeChangedCoverageBaseline(["owner"], ["owner"], previous, measured, required), {
    owner: { statements: 51, branches: 41, functions: 52, lines: 51 },
  });
});

test("coverage policy rejects weaker floors, debt progress, and new exclusions", () => {
  assert.deepEqual(coveragePolicyWeakening(legacyCoveragePolicy, legacyCoveragePolicy), []);
  assert.deepEqual(
    coveragePolicyWeakening(legacyCoveragePolicy, {
      ...legacyCoveragePolicy,
      floors: { ...legacyCoveragePolicy.floors, branches: 0 },
      debtStep: 0.5,
      excludedDirectories: [...legacyCoveragePolicy.excludedDirectories, "src/pages"],
    }),
    [
      "branches floor decreased from 60 to 0",
      "debtStep decreased from 1 to 0.5",
      "excludedDirectories added unreviewed exclusion src/pages",
    ],
  );
});

test("coverage policy validates every mutable field", () => {
  assert.throws(() => readCoveragePolicy({ ...legacyCoveragePolicy, debtStep: 0 }), /debtStep/);
  assert.throws(() => readCoveragePolicy({ ...legacyCoveragePolicy, excludedPatterns: [""] }), /excludedPatterns/);
});

function expectBaseline(actual, stable, changed) {
  assert.deepEqual(actual, { stable, changed });
}
