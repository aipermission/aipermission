import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { loadAndVerifyTestManifest } from "./test-manifest-policy.js";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const manifest = loadAndVerifyTestManifest();

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

function summaryCount(output, label) {
  const match = new RegExp(`^# ${label} (\\d+)$`, "m").exec(output);
  if (!match) throw new Error(`Node test output did not report ${label}`);
  return Number(match[1]);
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
const tests = summaryCount(result.stdout, "tests");
const skipped = summaryCount(result.stdout, "skipped");
if (tests < manifest.minimumTests) {
  throw new Error(`MCP suite executed ${tests} tests; expected at least ${manifest.minimumTests}`);
}
if (skipped > manifest.maximumSkipped) {
  throw new Error(`MCP suite skipped ${skipped} tests; maximum is ${manifest.maximumSkipped}`);
}
