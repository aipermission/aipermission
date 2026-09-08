import assert from "node:assert/strict";
import test from "node:test";

import NoSkippedPlaywrightReporter from "./no-skipped-playwright-reporter.mjs";

test("preserves a successful run when no Playwright test is skipped", () => {
  const reporter = new NoSkippedPlaywrightReporter();
  reporter.onBegin({}, suite([]));
  assert.deepEqual(reporter.onEnd({ status: "passed" }), { status: "passed" });
});

test("fails the run for skipped or expected-failure Playwright tests", () => {
  const originalError = console.error;
  console.error = () => {};
  try {
    const staticReporter = new NoSkippedPlaywrightReporter();
    staticReporter.onBegin({}, suite([fixtureTest("static skip", "skipped")]));
    assert.deepEqual(staticReporter.onEnd({ status: "passed" }), { status: "failed" });

    const dynamicReporter = new NoSkippedPlaywrightReporter();
    const dynamicTest = fixtureTest("dynamic skip", "passed");
    dynamicReporter.onBegin({}, suite([dynamicTest]));
    dynamicReporter.onTestEnd(dynamicTest, { status: "skipped" });
    assert.deepEqual(dynamicReporter.onEnd({ status: "passed" }), { status: "failed" });

    const expectedFailureReporter = new NoSkippedPlaywrightReporter();
    const expectedFailure = fixtureTest("expected failure", "passed");
    expectedFailureReporter.onBegin({}, suite([expectedFailure]));
    expectedFailure.expectedStatus = "failed";
    expectedFailureReporter.onTestEnd(expectedFailure, { status: "failed" });
    assert.deepEqual(expectedFailureReporter.onEnd({ status: "passed" }), { status: "failed" });
  } finally {
    console.error = originalError;
  }
});

function fixtureTest(title, expectedStatus) {
  return { expectedStatus, titlePath: () => ["chromium", title] };
}

function suite(tests) {
  return { allTests: () => tests };
}
