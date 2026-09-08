import { readFileSync } from "node:fs";

import { moduleSpecifiers, resolveSourceImport } from "./architecture-graph.mjs";

export function testReachesOwner({ graph, ownerPath, sourceFiles, sourceRoot, testPath }) {
  const direct = moduleSpecifiers(readFileSync(testPath, "utf8"))
    .map((specifier) => resolveSourceImport(sourceRoot, testPath, specifier, sourceFiles))
    .filter(Boolean);
  const visited = new Set();
  const pending = [...direct];
  while (pending.length > 0) {
    const candidate = pending.pop();
    if (candidate === ownerPath) return true;
    if (visited.has(candidate)) continue;
    visited.add(candidate);
    pending.push(...(graph.get(candidate) || []));
  }
  return false;
}
