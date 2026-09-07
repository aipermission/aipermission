import { resolve } from "node:path";

import { analyzeSourceTree } from "./architecture-graph.mjs";

const sourceRoot = resolve(process.cwd(), "src");
const result = analyzeSourceTree(sourceRoot, { importBudget: 20 });

if (result.failures.length > 0) {
  console.error("Frontend architecture budget failed:");
  result.failures.forEach((failure) => console.error(`- ${failure}`));
  process.exit(1);
}

console.log(
  `Frontend architecture budget passed: ${result.files.length} modules, no cycles, and dependency fan-out at most ${result.importBudget}.`,
);
