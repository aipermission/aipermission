import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { assertSourceArchiveMode, loadAndVerifyTestManifest, loadCurrentTestManifest } from "./test-manifest-policy.js";
import { nodeTestSummaryCount } from "./node-test-summary.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const currentOnly = process.argv.slice(2).includes("--current-only");
if (process.argv.slice(2).some((argument) => argument !== "--current-only")) {
  throw new Error("Unsupported MCP test argument.");
}
if (currentOnly) assertSourceArchiveMode(path.resolve(root, "../.."));
const manifest = currentOnly ? loadCurrentTestManifest() : loadAndVerifyTestManifest();

function discoverTests(directory) {
  return fs
    .readdirSync(directory, { withFileTypes: true })
    .flatMap((entry) => {
      const entryPath = path.join(directory, entry.name);
      return entry.isDirectory() ? discoverTests(entryPath) : [entryPath];
    })
    .filter((file) => file.endsWith(".test.js"))
    .map((file) => path.relative(root, file).replaceAll(path.sep, "/"))
    .sort();
}

const discovered = discoverTests(path.join(root, "test"));
const expected = [...manifest.files].sort();
if (JSON.stringify(discovered) !== JSON.stringify(expected)) {
  throw new Error(`MCP test manifest is stale.\nExpected: ${expected.join(", ")}\nFound: ${discovered.join(", ")}`);
}
const result = spawnSync(process.execPath, ["--test", ...expected], {
  cwd: root,
  encoding: "utf8",
  stdio: ["ignore", "pipe", "pipe"],
});
process.stdout.write(result.stdout || "");
process.stderr.write(result.stderr || "");
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status || 1);
const tests = nodeTestSummaryCount(result.stdout, "tests");
const skipped = nodeTestSummaryCount(result.stdout, "skipped");
if (tests < manifest.minimumTests) {
  throw new Error(`MCP suite executed ${tests} tests; expected at least ${manifest.minimumTests}`);
}
if (skipped > manifest.maximumSkipped) {
  throw new Error(`MCP suite skipped ${skipped} tests; maximum is ${manifest.maximumSkipped}`);
}
