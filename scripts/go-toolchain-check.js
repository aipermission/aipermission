const fs = require("node:fs");
const path = require("node:path");
const { workflowFiles } = require("./workflow-files");
const { workflowSetupGoVersions } = require("./workflow-contracts");

const repositoryRoot = path.resolve(__dirname, "..");
const requireMatches = (root, filename, pattern, label) => {
  const matches = [
    ...fs.readFileSync(path.join(root, filename), "utf8").matchAll(pattern),
  ];
  if (matches.length === 0) {
    throw new Error(`${filename} does not declare ${label}`);
  }
  return matches.map((match) => match[1]);
};

function verifyGoToolchain({ root = repositoryRoot } = {}) {
  const expected = requireMatches(
    root,
    "backend/go.mod",
    /^toolchain go([^\s]+)$/gm,
    "a Go toolchain",
  );
  if (expected.length !== 1 || !/^\d+\.\d+\.\d+$/.test(expected[0])) {
    throw new Error(
      "backend/go.mod must declare one exact Go toolchain version",
    );
  }
  const check = (filename, actual) => {
    if (actual !== expected[0]) {
      throw new Error(
        `${filename} uses Go ${actual || "without a version"}; expected ${expected[0]} from backend/go.mod`,
      );
    }
  };
  for (const filename of [
    "backend/Dockerfile",
    "backend/testdata/connector-conformance/Dockerfile",
  ]) {
    for (const version of requireMatches(
      root,
      filename,
      /^FROM(?:\s+--platform=\S+)?\s+golang:([^\s@-]+)/gim,
      "a Go builder image",
    )) {
      check(filename, version);
    }
  }
  let count = 0;
  for (const filename of workflowFiles(root)) {
    for (const version of workflowSetupGoVersions(
      fs.readFileSync(filename, "utf8"),
      path.relative(root, filename),
    )) {
      count++;
      check(path.relative(root, filename), version);
    }
  }
  if (count === 0)
    throw new Error("GitHub workflows do not declare actions/setup-go");
  return expected[0];
}

if (require.main === module) {
  try {
    process.stdout.write(
      `Go toolchain declarations match ${verifyGoToolchain()}.\n`,
    );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

module.exports = { verifyGoToolchain };
