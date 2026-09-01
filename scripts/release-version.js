#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const root = path.resolve(__dirname, "..");
const semverPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;
const pinnedMCPConfigDocs = [
  "README.md",
  "packages/mcp/README.md",
  "docs/setup/mcp-client-setup.md",
];
const dockerReleaseComposePath = "docker-compose.release.yml";
const dockerReleaseImagePattern = /(aipermission-(?:backend|frontend):\$\{AIPERMISSION_VERSION:-)([^}]+)(\})/g;

function readJSON(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(root, relativePath), "utf8"));
}

function stageJSON(updates, relativePath, value) {
  updates.set(relativePath, `${JSON.stringify(value, null, 2)}\n`);
}

function replaceRequired(source, pattern, replacement, label) {
  if (!pattern.test(source)) {
    throw new Error(`could not update ${label}`);
  }
  return source.replace(pattern, replacement);
}

function readStagedSource(updates, relativePath) {
  return (
    updates.get(relativePath) ||
    fs.readFileSync(path.join(root, relativePath), "utf8")
  );
}

function stagePinnedMCPConfigDocs(updates, version) {
  for (const relativePath of pinnedMCPConfigDocs) {
    const source = readStagedSource(updates, relativePath);
    const pattern = /@aipermission\/mcp@(?:VERSION|\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)/g;
    if (!pattern.test(source)) {
      throw new Error(`could not update pinned MCP config in ${relativePath}`);
    }
    updates.set(
      relativePath,
      source.replace(pattern, `@aipermission/mcp@${version}`),
    );
  }
}

function stagePinnedDockerCompose(updates, version) {
  const source = readStagedSource(updates, dockerReleaseComposePath);
  const matches = [...source.matchAll(dockerReleaseImagePattern)];
  if (matches.length !== 3) {
    throw new Error(`could not update all pinned Docker release images in ${dockerReleaseComposePath}`);
  }
  updates.set(
    dockerReleaseComposePath,
    source.replace(dockerReleaseImagePattern, (_match, prefix, _currentVersion, suffix) => `${prefix}${version}${suffix}`),
  );
}

function setVersion(version) {
  if (!semverPattern.test(version)) {
    throw new Error(`invalid release version: ${version}`);
  }

  const updates = new Map();
  stageJSON(updates, "release-manifest.json", { version });

  const frontendPackage = readJSON("frontend/package.json");
  frontendPackage.version = version;
  stageJSON(updates, "frontend/package.json", frontendPackage);

  const frontendLock = readJSON("frontend/package-lock.json");
  frontendLock.version = version;
  frontendLock.packages[""].version = version;
  stageJSON(updates, "frontend/package-lock.json", frontendLock);

  const mcpPackage = readJSON("packages/mcp/package.json");
  mcpPackage.version = version;
  stageJSON(updates, "packages/mcp/package.json", mcpPackage);

  const mcpLock = readJSON("packages/mcp/package-lock.json");
  mcpLock.version = version;
  mcpLock.packages[""].version = version;
  stageJSON(updates, "packages/mcp/package-lock.json", mcpLock);

  const mcpServer = readJSON("packages/mcp/server.json");
  mcpServer.version = version;
  for (const packageEntry of mcpServer.packages || []) {
    packageEntry.version = version;
  }
  stageJSON(updates, "packages/mcp/server.json", mcpServer);
  stagePinnedMCPConfigDocs(updates, version);
  stagePinnedDockerCompose(updates, version);

  const backendVersionPath = path.join(
    root,
    "backend/internal/buildinfo/version.go",
  );
  const backendVersionSource = fs.readFileSync(backendVersionPath, "utf8");
  updates.set(
    "backend/internal/buildinfo/version.go",
    replaceRequired(
      backendVersionSource,
      /^const Version = "[^"]+"$/m,
      `const Version = "${version}"`,
      "backend build version",
    ),
  );
  commitFileUpdates(root, updates);
}

function pinnedDockerReleaseValue(source, version) {
  const matches = [...source.matchAll(dockerReleaseImagePattern)];
  if (matches.length !== 3) return undefined;
  const versions = matches.map((match) => match[2]);
  return versions.every((item) => item === version) ? version : versions.join(",");
}

function commitFileUpdates(baseDirectory, updates, operations = {}) {
  const writeFile = operations.writeFileSync || fs.writeFileSync;
  const rename = operations.renameSync || fs.renameSync;
  const unlink = operations.unlinkSync || fs.unlinkSync;
  const originals = new Map();
  const prepared = [];
  const committed = [];
  const temporaryPaths = new Set();
  let rollbackError;

  const removeTemporary = (temporaryPath) => {
    try {
      unlink(temporaryPath);
    } catch (error) {
      if (error.code !== "ENOENT") throw error;
    } finally {
      temporaryPaths.delete(temporaryPath);
    }
  };

  const writeTemporary = (temporaryPath, contents, mode) => {
    temporaryPaths.add(temporaryPath);
    writeFile(temporaryPath, contents, { mode });
  };

  try {
    let index = 0;
    for (const [relativePath, contents] of updates) {
      const targetPath = path.join(baseDirectory, relativePath);
      const stat = fs.statSync(targetPath);
      originals.set(targetPath, fs.readFileSync(targetPath));
      const temporaryPath = `${targetPath}.aipermission-release-${process.pid}-${index}.tmp`;
      prepared.push({ targetPath, temporaryPath, mode: stat.mode });
      writeTemporary(temporaryPath, contents, stat.mode);
      index += 1;
    }
    for (const item of prepared) {
      rename(item.temporaryPath, item.targetPath);
      temporaryPaths.delete(item.temporaryPath);
      committed.push(item);
    }
  } catch (error) {
    for (const temporaryPath of [...temporaryPaths]) {
      try {
        removeTemporary(temporaryPath);
      } catch (cleanupError) {
        rollbackError ||= cleanupError;
      }
    }
    for (const item of committed.reverse()) {
      const rollbackPath = `${item.targetPath}.aipermission-release-rollback-${process.pid}.tmp`;
      try {
        writeTemporary(rollbackPath, originals.get(item.targetPath), item.mode);
        rename(rollbackPath, item.targetPath);
        temporaryPaths.delete(rollbackPath);
      } catch (restoreError) {
        rollbackError ||= restoreError;
      } finally {
        try {
          removeTemporary(rollbackPath);
        } catch (cleanupError) {
          rollbackError ||= cleanupError;
        }
      }
    }
    if (rollbackError) {
      throw new Error(
        `release metadata update failed (${error.message}); rollback also failed (${rollbackError.message})`,
      );
    }
    throw error;
  } finally {
    for (const temporaryPath of [...temporaryPaths]) {
      try {
        removeTemporary(temporaryPath);
      } catch (cleanupError) {
        rollbackError ||= cleanupError;
      }
    }
  }
  if (rollbackError) throw rollbackError;
}

function checkVersion() {
  execFileSync(
    process.execPath,
    [path.join(root, "scripts/release-notes.js"), "--check"],
    {
      stdio: "inherit",
    },
  );
  const version = readJSON("release-manifest.json").version;
  if (!semverPattern.test(version)) {
    throw new Error(
      `release-manifest.json contains an invalid version: ${version}`,
    );
  }

  const values = [
    ["frontend/package.json", readJSON("frontend/package.json").version],
    [
      "frontend/package-lock.json",
      readJSON("frontend/package-lock.json").version,
    ],
    [
      "frontend/package-lock.json packages root",
      readJSON("frontend/package-lock.json").packages[""].version,
    ],
    [
      "packages/mcp/package.json",
      readJSON("packages/mcp/package.json").version,
    ],
    [
      "packages/mcp/package-lock.json",
      readJSON("packages/mcp/package-lock.json").version,
    ],
    [
      "packages/mcp/package-lock.json packages root",
      readJSON("packages/mcp/package-lock.json").packages[""].version,
    ],
    ["packages/mcp/server.json", readJSON("packages/mcp/server.json").version],
  ];
  for (const [index, packageEntry] of (
    readJSON("packages/mcp/server.json").packages || []
  ).entries()) {
    values.push([
      `packages/mcp/server.json packages[${index}]`,
      packageEntry.version,
    ]);
  }

  const frontendRelease = readJSON("frontend/src/lib/release.generated.json");
  values.push(["frontend appVersion", frontendRelease.version]);
  values.push([
    "frontend latest changelog entry",
    frontendRelease.entries?.[0]?.version,
  ]);
  values.push([
    "canonical latest release note",
    readJSON("release-notes.json").releases?.[0]?.version,
  ]);

  const backendVersionSource = fs.readFileSync(
    path.join(root, "backend/internal/buildinfo/version.go"),
    "utf8",
  );
  values.push([
    "backend build version",
    backendVersionSource.match(/^const Version = "([^"]+)"$/m)?.[1],
  ]);
  for (const relativePath of pinnedMCPConfigDocs) {
    const source = fs.readFileSync(path.join(root, relativePath), "utf8");
    const configuredVersion = source.match(
      /@aipermission\/mcp@(VERSION|\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)/,
    )?.[1];
    values.push([
      `${relativePath} pinned MCP config`,
      configuredVersion === "VERSION" ? version : configuredVersion,
    ]);
  }
  const dockerComposeSource = fs.readFileSync(path.join(root, dockerReleaseComposePath), "utf8");
  values.push([dockerReleaseComposePath, pinnedDockerReleaseValue(dockerComposeSource, version)]);

  const changelog = fs.readFileSync(path.join(root, "CHANGELOG.md"), "utf8");
  values.push([
    "CHANGELOG latest release",
    changelog.match(/^## \[([^\]]+)\] - /m)?.[1],
  ]);

  const failures = values.filter(([, value]) => value !== version);
  if (failures.length > 0) {
    throw new Error(
      `release version ${version} is inconsistent:\n${failures.map(([label, value]) => `- ${label}: ${value || "missing"}`).join("\n")}`,
    );
  }
  console.log(`Release version metadata is consistent at ${version}.`);
}

if (require.main === module) {
  try {
    if (process.argv[2] === "--set") {
      setVersion(process.argv[3] || "");
      console.log(
        "Release package metadata updated. Add the matching canonical release note, run node scripts/release-notes.js, then run --check.",
      );
    } else if (process.argv.length > 2 && process.argv[2] !== "--check") {
      throw new Error("usage: release-version.js [--check | --set X.Y.Z]");
    } else {
      checkVersion();
    }
  } catch (error) {
    console.error(`Release version check failed: ${error.message}`);
    process.exit(1);
  }
}

module.exports = {
  commitFileUpdates,
  pinnedDockerReleaseValue,
  stagePinnedDockerCompose,
  stagePinnedMCPConfigDocs,
};
