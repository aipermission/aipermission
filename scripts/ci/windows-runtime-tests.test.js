const assert = require("node:assert/strict");
const test = require("node:test");

const {
  requiredTests,
  verifyRequiredTestEvents,
} = require("./windows-runtime-tests");

const event = (name, action) => JSON.stringify({ Test: name, Action: action });

test("Windows runtime evidence requires one pass for every named test", () => {
  const lines = requiredTests.map((name) => event(name, "pass"));
  assert.deepEqual(verifyRequiredTestEvents(lines), []);
});

test("Windows runtime evidence rejects missing, skipped, failed, and duplicate terminals", () => {
  const [passed, skipped, failed] = requiredTests;
  const lines = [
    event(passed, "pass"),
    event(passed, "pass"),
    event(skipped, "skip"),
    event(failed, "fail"),
  ];
  assert.deepEqual(verifyRequiredTestEvents(lines), requiredTests);
  assert.deepEqual(verifyRequiredTestEvents([], requiredTests), requiredTests);
});
