#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { workflowSetupNodeVersions } = require("./workflow-contracts");

const repositoryRoot = path.resolve(__dirname, "..");

function readExactVersion(root) {
  const value = fs
    .readFileSync(path.join(root, ".node-version"), "utf8")
    .trim();
  if (!/^\d+\.\d+\.\d+$/.test(value)) {
    throw new Error(".node-version must contain one exact semantic version");
  }
  return value;
}

function workflowFiles(root) {
  const directory = path.join(root, ".github", "workflows");
  return fs
    .readdirSync(directory, { withFileTypes: true })
    .filter((entry) => entry.isFile() && /\.ya?ml$/i.test(entry.name))
    .map((entry) => path.join(directory, entry.name))
    .sort();
}

function readPinnedVersion(root, filename) {
  const value = fs.readFileSync(path.join(root, filename), "utf8").trim();
  if (!/^\d+\.\d+\.\d+$/.test(value)) {
    throw new Error(`${filename} must contain one exact semantic version`);
  }
  return value;
}

function frontendBuilderVersion(root) {
  const dockerfile = fs.readFileSync(path.join(root, "frontend", "Dockerfile"), "utf8");
  const match = /^FROM\s+node:(\d+\.\d+\.\d+)-[^\s@]+@sha256:[0-9a-f]{64}\s+AS\s+build\s*$/im.exec(dockerfile);
  if (!match) {
    throw new Error("frontend/Dockerfile must pin its Node.js builder to an exact version and sha256 digest");
  }
  return match[1];
}

function verifyNodeToolchain({
  root = repositoryRoot,
  runtimeVersion = process.versions.node,
} = {}) {
  const expected = readExactVersion(root);
  const nvmVersion = readPinnedVersion(root, ".nvmrc");
  if (nvmVersion !== expected) {
    throw new Error(`.nvmrc uses Node.js ${nvmVersion}; expected ${expected} from .node-version`);
  }
  if (runtimeVersion !== expected) {
    throw new Error(
      `Node.js ${runtimeVersion} is active; expected ${expected} from .node-version. Run \`nvm install && nvm use\` before repository checks.`,
    );
  }
  let setupCount = 0;
  for (const file of workflowFiles(root)) {
    const relative = path.relative(root, file);
    for (const actual of workflowSetupNodeVersions(fs.readFileSync(file, "utf8"), relative)) {
      setupCount++;
      if (String(actual) !== expected) {
        throw new Error(
          `${path.relative(root, file)} uses Node.js ${actual || "without a version"}; expected ${expected} from .node-version`,
        );
      }
    }
  }
  if (setupCount === 0) {
    throw new Error("GitHub workflows do not declare actions/setup-node");
  }
  const builderVersion = frontendBuilderVersion(root);
  if (builderVersion !== expected) {
    throw new Error(`frontend/Dockerfile uses Node.js ${builderVersion}; expected ${expected} from .node-version`);
  }
  const contributing = fs.readFileSync(
    path.join(root, "CONTRIBUTING.md"),
    "utf8",
  );
  if (!contributing.includes(`Node.js ${expected}`)) {
    throw new Error(`CONTRIBUTING.md must document Node.js ${expected}`);
  }
  return expected;
}

if (require.main === module) {
  try {
    const version = verifyNodeToolchain();
    process.stdout.write(`Node.js toolchain declarations match ${version}.\n`);
  } catch (error) {
    console.error(error.message);
    process.exit(1);
  }
}

module.exports = { verifyNodeToolchain };
