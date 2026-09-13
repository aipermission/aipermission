#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

function discoverTestFiles(directories) {
  const files = directories.flatMap((directory) =>
    fs
      .readdirSync(directory, { withFileTypes: true })
      .filter((entry) => entry.isFile() && entry.name.endsWith(".test.js"))
      .map((entry) => path.join(directory, entry.name)),
  );
  if (files.length === 0) throw new Error("no tooling tests discovered");
  return files.sort();
}

function run(directories) {
  const result = spawnSync(
    process.execPath,
    ["--test", ...discoverTestFiles(directories)],
    { stdio: "inherit" },
  );
  if (result.error) throw result.error;
  return result.status ?? 1;
}

if (require.main === module) {
  try {
    process.exitCode = run(process.argv.slice(2));
  } catch (error) {
    console.error(`Tooling test discovery failed: ${error.message}`);
    process.exitCode = 1;
  }
}

module.exports = { discoverTestFiles, run };
