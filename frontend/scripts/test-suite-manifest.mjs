import { globSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import { asyncStateTestIncludes, riskCoverageTestIncludes } from "../test-suite-manifests.mjs";

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const suites = {
  "async-state": asyncStateTestIncludes,
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
console.log(`Frontend test suite manifest passed for ${Object.keys(suites).length} suites.`);
