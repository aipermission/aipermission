#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const semverPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;

function readJSON(relativePath) {
  return JSON.parse(fs.readFileSync(path.join(root, relativePath), "utf8"));
}

function writeJSON(relativePath, value) {
  fs.writeFileSync(
    path.join(root, relativePath),
    `${JSON.stringify(value, null, 2)}\n`,
  );
}

function replaceRequired(source, pattern, replacement, label) {
  if (!pattern.test(source)) {
    throw new Error(`could not update ${label}`);
  }
  return source.replace(pattern, replacement);
}

function setVersion(version) {
  if (!semverPattern.test(version)) {
    throw new Error(`invalid release version: ${version}`);
  }

  writeJSON("release-manifest.json", { version });

  const frontendPackage = readJSON("frontend/package.json");
  frontendPackage.version = version;
  writeJSON("frontend/package.json", frontendPackage);

  const frontendLock = readJSON("frontend/package-lock.json");
  frontendLock.version = version;
  frontendLock.packages[""].version = version;
  writeJSON("frontend/package-lock.json", frontendLock);

  const rootLock = readJSON("package-lock.json");
  rootLock.packages.frontend.version = version;
  writeJSON("package-lock.json", rootLock);

  const mcpPackage = readJSON("packages/mcp/package.json");
  mcpPackage.version = version;
  writeJSON("packages/mcp/package.json", mcpPackage);

  const mcpLock = readJSON("packages/mcp/package-lock.json");
  mcpLock.version = version;
  mcpLock.packages[""].version = version;
  writeJSON("packages/mcp/package-lock.json", mcpLock);

  const mcpServer = readJSON("packages/mcp/server.json");
  mcpServer.version = version;
  for (const packageEntry of mcpServer.packages || []) {
    packageEntry.version = version;
  }
  writeJSON("packages/mcp/server.json", mcpServer);

  const releasePath = path.join(root, "frontend/src/lib/release.js");
  const releaseSource = fs.readFileSync(releasePath, "utf8");
  fs.writeFileSync(
    releasePath,
    replaceRequired(
      releaseSource,
      /^export const appVersion = "[^"]+";/m,
      `export const appVersion = "${version}";`,
      "frontend appVersion",
    ),
  );
}

function checkVersion() {
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
      "root package-lock frontend workspace",
      readJSON("package-lock.json").packages.frontend.version,
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

  const releaseSource = fs.readFileSync(
    path.join(root, "frontend/src/lib/release.js"),
    "utf8",
  );
  values.push([
    "frontend appVersion",
    releaseSource.match(/^export const appVersion = "([^"]+)";/m)?.[1],
  ]);
  values.push([
    "frontend latest changelog entry",
    releaseSource.match(
      /changelogEntries = \[\s*\{\s*version: "([^"]+)"/s,
    )?.[1],
  ]);

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

try {
  if (process.argv[2] === "--set") {
    setVersion(process.argv[3] || "");
    console.log(
      "Release package metadata updated. Add the matching changelog and in-app release entry, then run --check.",
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
