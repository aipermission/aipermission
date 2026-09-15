#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

const root = path.resolve(__dirname, "..");
const policy = require("../maintenance-policy.json");
const toolingBudget = policy.sourceBudgets.find(
  (budget) => budget.id === "repository-tooling",
);
const toolingExtensions = new Set(toolingBudget.extensions);
const toolingMarkers = policy.frontendArchitecture.testModuleMarkers;

function ownedBy(file, owners) {
  return owners.filter((directory) => {
    const relative = path.relative(directory, file);
    return relative !== "" && !relative.startsWith(`..${path.sep}`);
  });
}

function discoverTestFiles(directories) {
  const files = directories.flatMap(walkTestFiles);
  if (files.length === 0) throw new Error("no tooling tests discovered");
  return files.sort();
}

function walkTestFiles(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const candidate = path.join(directory, entry.name);
    if (entry.isDirectory()) {
      return entry.name === "node_modules" ? [] : walkTestFiles(candidate);
    }
    if (entry.isSymbolicLink()) {
      throw new Error(`tooling test symlink is not allowed: ${candidate}`);
    }
    if (!entry.isFile()) return [];
    const extension = path.extname(entry.name);
    const stem = path.basename(entry.name, extension);
    return toolingExtensions.has(extension) &&
      toolingMarkers.some((marker) => stem.endsWith(marker.slice(0, -1)))
      ? [candidate]
      : [];
  });
}

function verifyTestInventory(
  files,
  directories,
  expected = policy.toolingTestFiles,
  repositoryRoot = root,
  configured = policy.toolingTestRoots.map((directory) =>
    path.join(repositoryRoot, directory),
  ),
) {
  const roots = directories.map((directory) => path.resolve(directory));
  const configuredRoots = configured.map((directory) =>
    path.resolve(directory),
  );
  for (const directory of roots) {
    if (!configuredRoots.includes(directory)) {
      throw new Error(`unregistered tooling test root: ${directory}`);
    }
  }
  const relative = (file) =>
    path.relative(repositoryRoot, path.resolve(file)).split(path.sep).join("/");
  for (const file of expected) {
    const owners = ownedBy(path.join(repositoryRoot, file), configuredRoots);
    if (owners.length !== 1) {
      throw new Error(
        `tooling test must have exactly one configured root: ${file}`,
      );
    }
  }
  const discovered = files.map(relative).sort();
  const selected = expected
    .filter(
      (file) => ownedBy(path.join(repositoryRoot, file), roots).length === 1,
    )
    .sort();
  const missing = selected.filter((file) => !discovered.includes(file));
  const unexpected = discovered.filter((file) => !selected.includes(file));
  if (missing.length > 0 || unexpected.length > 0) {
    throw new Error(
      [
        missing.length > 0 ? `missing: ${missing.join(", ")}` : "",
        unexpected.length > 0 ? `unregistered: ${unexpected.join(", ")}` : "",
      ]
        .filter(Boolean)
        .join("; "),
    );
  }
  return files;
}

function resolveTestRoots(
  requested,
  repositoryRoot = root,
  configured = policy.toolingTestRoots.map((directory) =>
    path.join(repositoryRoot, directory),
  ),
) {
  const configuredRoots = configured.map((directory) =>
    path.resolve(directory),
  );
  if (requested.length === 0) {
    throw new Error("at least one tooling test root is required");
  }
  const requestedRoots = requested.map((directory) => path.resolve(directory));
  for (const resolved of requestedRoots) {
    if (!configuredRoots.includes(resolved)) {
      throw new Error(`unregistered tooling test root: ${resolved}`);
    }
  }
  return requestedRoots;
}

function run(requested, options = {}) {
  const repositoryRoot = options.repositoryRoot || root;
  const configured =
    options.configured ||
    policy.toolingTestRoots.map((directory) =>
      path.join(repositoryRoot, directory),
    );
  const expected = options.expected || policy.toolingTestFiles;
  const directories = resolveTestRoots(requested, repositoryRoot, configured);
  const inventoryRoot = path.join(repositoryRoot, toolingBudget.directory);
  const files = discoverTestFiles([inventoryRoot]);
  verifyTestInventory(files, configured, expected, repositoryRoot, configured);
  const selected = files.filter((file) => ownedBy(file, directories).length === 1);
  if (selected.length === 0) throw new Error("no tooling tests selected");
  const execute = options.spawnSync || spawnSync;
  const result = execute(process.execPath, ["--test", ...selected], {
    stdio: "inherit",
  });
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

module.exports = {
  discoverTestFiles,
  resolveTestRoots,
  run,
  verifyTestInventory,
};
