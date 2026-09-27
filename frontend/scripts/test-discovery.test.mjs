import assert from "node:assert/strict";
import test from "node:test";
import { readdirSync, readFileSync } from "node:fs";
import { isNodeUnitTest } from "./test-discovery.mjs";

test("all frontend source and colocated tests remain under strict TypeScript", () => {
  function sourceFiles(directory) {
    return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
      const path = new URL(`${entry.name}${entry.isDirectory() ? "/" : ""}`, directory);
      return entry.isDirectory() ? sourceFiles(path) : [path.pathname];
    });
  }
  assert.deepEqual(
    sourceFiles(new URL("../src/", import.meta.url)).filter((path) => /\.[cm]?jsx?$/.test(path)),
    [],
  );
  const config = JSON.parse(readFileSync(new URL("../tsconfig.json", import.meta.url), "utf8"));
  assert.equal(config.compilerOptions.strict, true);
  assert.notEqual(config.compilerOptions.allowJs, true);
  assert.deepEqual(config.include, ["src/**/*.ts", "src/**/*.tsx", "types/**/*.ts"]);
  assert.equal(config.exclude, undefined, "colocated source tests must not escape compiler checks");
});

test("discovers JavaScript and TypeScript Node unit suites", () => {
  for (const name of ["policy.test.js", "policy.test.ts", "connector-registry-runtime.test.ts"]) {
    assert.equal(isNodeUnitTest(name), true, name);
  }
});

test("leaves component tests with Vitest and declaration checks with TypeScript", () => {
  for (const name of [
    "policy.component.test.js",
    "policy.component.test.ts",
    "policy.component.test.jsx",
    "policy.component.test.tsx",
    "policy.type.test.ts",
    "policy.type.test.js",
    "policy.ts",
    "policy.test.tsx",
    "policy.test.js.map",
  ]) {
    assert.equal(isNodeUnitTest(name), false, name);
  }
});
