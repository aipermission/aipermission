import { readdirSync } from "node:fs";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import { isNodeUnitTest } from "./test-discovery.mjs";

const files = testFiles(join(process.cwd(), "src"));
// Keep the real browser/Vite registry check out of the parallel Node worker pool.
const serialFiles = new Set([join(process.cwd(), "src", "lib", "connector-registry-runtime.test.ts")]);
const suites = [files.filter((file) => !serialFiles.has(file)), files.filter((file) => serialFiles.has(file))];

for (const suite of suites) {
  if (suite.length === 0) continue;
  const result = spawnSync(process.execPath, ["--test", ...suite], { stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) process.exit(result.status ?? 1);
}

function testFiles(root) {
  const files = [];
  for (const entry of readdirSync(root, { withFileTypes: true })) {
    const path = join(root, entry.name);
    if (entry.isDirectory()) files.push(...testFiles(path));
    else if (isNodeUnitTest(entry.name)) files.push(path);
  }
  return files.sort();
}
