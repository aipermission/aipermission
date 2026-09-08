import { globSync, readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { analyzeSourceTree } from "./architecture-graph.mjs";
import { testReachesOwner } from "./async-owner-manifest.mjs";
import { isAsyncStateOwner } from "./async-owner-policy.mjs";
import { asyncStateOwnerTests, asyncStateTestIncludes, riskCoverageTestIncludes } from "../test-suite-manifests.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const suites = {
  "async-state": asyncStateTestIncludes,
  "async-state-owner-tests": [...new Set(Object.values(asyncStateOwnerTests).flat())],
  "risk-coverage": riskCoverageTestIncludes,
};
const missing = Object.entries(suites).flatMap(([suite, patterns]) =>
  patterns.filter((pattern) => globSync(pattern, { cwd: frontendRoot }).length === 0).map((pattern) => `${suite}: ${pattern}`),
);
if (missing.length > 0) {
  console.error("Frontend test suite manifest contains unmatched patterns:");
  missing.forEach((entry) => console.error(`- ${entry}`));
  process.exit(1);
}
const sourceRoot = resolve(frontendRoot, "src");
const sourceAnalysis = analyzeSourceTree(sourceRoot);
const sourceFiles = new Set(sourceAnalysis.files);
const detectedAsyncOwners = sourceAnalysis.files
  .map((file) => `src/${file.slice(sourceRoot.length + 1).replaceAll("\\", "/")}`)
  .filter((file) => isAsyncStateOwner(readFileSync(resolve(frontendRoot, file), "utf8")))
  .sort();
const declaredAsyncOwners = Object.keys(asyncStateOwnerTests).sort();
const unowned = detectedAsyncOwners.filter((file) => !Object.hasOwn(asyncStateOwnerTests, file));
const stale = declaredAsyncOwners.filter((file) => !detectedAsyncOwners.includes(file));
if (unowned.length > 0 || stale.length > 0) {
  console.error("Frontend async-state owner manifest is out of date:");
  unowned.forEach((file) => console.error(`- missing owner mapping: ${file}`));
  stale.forEach((file) => console.error(`- stale owner mapping: ${file}`));
  process.exit(1);
}
const unsupportedMappings = Object.entries(asyncStateOwnerTests).flatMap(([owner, tests]) => {
  return tests.some((testFile) =>
    testReachesOwner({
      graph: sourceAnalysis.graph,
      ownerPath: resolve(frontendRoot, owner),
      sourceFiles,
      sourceRoot,
      testPath: resolve(frontendRoot, testFile),
    }),
  )
    ? []
    : [owner];
});
if (unsupportedMappings.length > 0) {
  console.error("Frontend async-state owner mappings lack an import-graph path from their declared tests:");
  unsupportedMappings.forEach((file) => console.error(`- unsupported owner mapping: ${file}`));
  process.exit(1);
}
console.log(`Frontend test suite manifest passed for ${Object.keys(suites).length} suites.`);
