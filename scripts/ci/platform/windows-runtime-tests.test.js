const assert = require("node:assert/strict");
const test = require("node:test");

const runtime = require("../windows-runtime-tests");

const event = (entry, action, packageName = entry.package) =>
  JSON.stringify({ Package: packageName, Test: entry.name, Action: action });

test("Windows runtime suite compiles the complete test graph", () => {
  assert.deepEqual(runtime.compileArguments, ["test", "-count=1", "-run", "^$", "./..."]);
});

test("Windows runtime evidence rejects every non-Windows host", () => {
  assert.doesNotThrow(() => runtime.assertWindowsPlatform("win32"));
  for (const platform of ["linux", "darwin"]) {
    assert.throws(() => runtime.assertWindowsPlatform(platform), /requires win32/);
  }
});

test("Windows runtime evidence requires one package-bound pass per test", () => {
  const passing = runtime.requiredTests.map((entry) => event(entry, "pass"));
  assert.deepEqual(runtime.verifyRequiredTestEvents(passing), []);
  const [passed, skipped, failed] = runtime.requiredTests;
  const invalid = [event(passed, "pass"), event(passed, "pass"), event(skipped, "skip"), event(failed, "fail")];
  const expected = runtime.requiredTests.map((entry) => `${entry.package}:${entry.name}`);
  assert.deepEqual(runtime.verifyRequiredTestEvents(invalid), expected);
  assert.deepEqual(runtime.verifyRequiredTestEvents([], runtime.requiredTests), expected);
  const lookalikes = runtime.requiredTests.map((entry) => event(entry, "pass", `${entry.package}/lookalike`));
  assert.deepEqual(runtime.verifyRequiredTestEvents(lookalikes), expected);
});

test("Windows runtime evidence enforces per-source coverage floors", () => {
  const lines = ["mode: atomic"];
  for (const sourcePath of Object.keys(runtime.requiredCoverage)) {
    lines.push(`github.com/aipermission/aipermission/backend/${sourcePath}:1.1,2.1 1 1`);
  }
  const coverage = runtime.parseCoverageProfile(lines.join("\n"));
  assert.deepEqual(runtime.verifyPlatformCoverage(coverage), []);
  const [sourcePath] = Object.keys(runtime.requiredCoverage);
  const expected = `${sourcePath}: 0.0% is below ${runtime.requiredCoverage[sourcePath].minimumCoverage.toFixed(1)}%`;
  coverage.set(sourcePath, { statements: 2, covered: 0 });
  assert.deepEqual(runtime.verifyPlatformCoverage(coverage), [expected]);
  coverage.delete(sourcePath);
  assert.deepEqual(runtime.verifyPlatformCoverage(coverage), [expected]);
});

test("Windows coverage parser rejects malformed profiles", () => {
  for (const content of ["", "mode: invalid\nsource.go:1.1,2.1 1 1", "mode: atomic\nbad line"]) {
    assert.throws(() => runtime.parseCoverageProfile(content), /invalid/);
  }
});
