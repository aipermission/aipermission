import assert from "node:assert/strict";
import test from "node:test";

import { coveragePolicyWeakening, legacyCoveragePolicy } from "../coverage-policy.mjs";

test("only accepts replacing the generated MCP catalog exclusion during its TypeScript migration", () => {
  const renamed = { ...legacyCoveragePolicy, excludedNames: ["mcp-client-catalog.ts", "release.generated.json"] };
  assert.deepEqual(coveragePolicyWeakening(legacyCoveragePolicy, renamed), []);
  assert.notDeepEqual(
    coveragePolicyWeakening(legacyCoveragePolicy, { ...renamed, excludedNames: [...renamed.excludedNames, "mcp-client-catalog.js"] }),
    [],
  );
  assert.notDeepEqual(
    coveragePolicyWeakening(legacyCoveragePolicy, { ...renamed, excludedNames: [...renamed.excludedNames, "api.ts"] }),
    [],
  );
});
import { coverageMetricsForOwner, mergeChangedCoverageBaseline, ratchetedMetrics } from "../coverage-ratchet.mjs";

const previous = { statements: 48, branches: 36, functions: 52, lines: 49 };
const measured = { statements: 50, branches: 40, functions: 55, lines: 51 };

test("extension-only TypeScript migration keeps the existing coverage debt", () => {
  const oldFiles = { "src/example.jsx": previous };
  const owners = ["src/example.tsx"];
  assert.deepEqual(coverageMetricsForOwner(oldFiles, owners[0], owners), previous);
  assert.deepEqual(ratchetedMetrics(coverageMetricsForOwner(oldFiles, owners[0], owners)), {
    statements: 49,
    branches: 37,
    functions: 53,
    lines: 50,
  });
  assert.deepEqual(mergeChangedCoverageBaseline(owners, owners, oldFiles, { [owners[0]]: measured }), { [owners[0]]: measured });
});

test("a new same-stem module cannot inherit a live owner's coverage", () => {
  const oldFiles = { "src/example.js": previous };
  assert.equal(coverageMetricsForOwner(oldFiles, "src/example.ts", ["src/example.js", "src/example.ts"]), undefined);
});

test("ambiguous extension migrations fail closed", () => {
  const oldFiles = { "src/example.js": previous, "src/example.jsx": previous };
  assert.throws(() => coverageMetricsForOwner(oldFiles, "src/example.tsx", ["src/example.tsx"]), /Ambiguous/);
});

test("only the typed manifest exclusion can replace its JSX predecessor", () => {
  const typedManifest = "^src/connectors/templates/[^/]+/index\\.ts$";
  assert.deepEqual(coveragePolicyWeakening(legacyCoveragePolicy, { ...legacyCoveragePolicy, excludedPatterns: [typedManifest] }), []);
  assert.deepEqual(
    coveragePolicyWeakening(legacyCoveragePolicy, { ...legacyCoveragePolicy, excludedPatterns: [typedManifest, "^src/pages/.*$"] }),
    ["excludedPatterns added unreviewed exclusion ^src/pages/.*$"],
  );
});
