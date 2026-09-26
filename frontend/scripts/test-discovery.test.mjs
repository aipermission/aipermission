import assert from "node:assert/strict";
import test from "node:test";
import { isNodeUnitTest } from "./test-discovery.mjs";

test("discovers JavaScript and TypeScript Node unit suites", () => {
  for (const name of ["policy.test.js", "policy.test.ts", "connector-registry-runtime.test.js"]) {
    assert.equal(isNodeUnitTest(name), true, name);
  }
});

test("leaves component tests with Vitest and declaration checks with TypeScript", () => {
  for (const name of [
    "policy.component.test.js", "policy.component.test.ts", "policy.component.test.jsx", "policy.component.test.tsx",
    "policy.type.test.ts", "policy.type.test.js", "policy.ts", "policy.test.tsx", "policy.test.js.map",
  ]) {
    assert.equal(isNodeUnitTest(name), false, name);
  }
});
