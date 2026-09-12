import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { analyzeSourceTree } from "./architecture-graph.mjs";

const sourceRoot = resolve(process.cwd(), "src");
const policy = JSON.parse(readFileSync(resolve(process.cwd(), "../maintenance-policy.json"), "utf8")).frontendArchitecture;
const result = analyzeSourceTree(sourceRoot, {
  importBudget: policy.maxDependencyFanout,
  lineBudget: policy.maxProductionModuleLines,
});

if (result.failures.length > 0) {
  console.error("Frontend architecture budget failed:");
  result.failures.forEach((failure) => console.error(`- ${failure}`));
  process.exit(1);
}

console.log(
  `Frontend architecture budget passed: ${result.files.length} modules, no cycles, at most ${result.lineBudget} lines, and dependency fan-out at most ${result.importBudget}.`,
);
