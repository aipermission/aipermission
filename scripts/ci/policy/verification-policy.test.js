const assert = require("node:assert/strict");
const test = require("node:test");

const { verifyWorkflows } = require("../../verification-policy");

test("CI workflows preserve the required verification contract", () => {
  assert.doesNotThrow(() => verifyWorkflows());
});
