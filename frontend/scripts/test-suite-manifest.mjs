import { globSync, readFileSync } from "node:fs";
import { basename, dirname, extname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

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
const asyncOwnerMarkers =
  /\b(?:useRequestGuard|createRequestGuard|AbortController|WebSocket|setInterval|cooldownTimers)\b|\brequestGuard\.(?:begin|invalidate)\s*\(|\bsetTimeout\s*\(\s*poll\b/;
const detectedAsyncOwners = globSync("src/**/*.{js,jsx}", { cwd: frontendRoot })
  .filter((file) => !/\.(?:component\.)?test\.[jt]sx?$/.test(file))
  .filter((file) => asyncOwnerMarkers.test(readFileSync(resolve(frontendRoot, file), "utf8")))
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
  const ownerStem = basename(owner, extname(owner));
  const marker = `async-owner: ${owner}`;
  return tests.some((testFile) => {
    const source = readFileSync(resolve(frontendRoot, testFile), "utf8");
    return source.includes(ownerStem) || source.includes(marker);
  })
    ? []
    : [owner];
});
if (unsupportedMappings.length > 0) {
  console.error("Frontend async-state owner mappings lack direct imports or explicit coverage markers:");
  unsupportedMappings.forEach((file) => console.error(`- unsupported owner mapping: ${file}`));
  process.exit(1);
}
console.log(`Frontend test suite manifest passed for ${Object.keys(suites).length} suites.`);
