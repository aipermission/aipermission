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

function updatePinnedMCPConfigDocs(version) {
  for (const relativePath of pinnedMCPConfigDocs) {
    const filePath = path.join(root, relativePath);
    const source = fs.readFileSync(filePath, "utf8");
    const pattern = /@aipermission\/mcp@\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?/g;
    if (!pattern.test(source)) {
      throw new Error(`could not update pinned MCP config in ${relativePath}`);
    }
    fs.writeFileSync(
      filePath,
      source.replace(pattern, `@aipermission/mcp@${version}`),
    );
  }
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
  updatePinnedMCPConfigDocs(version);

  const backendVersionPath = path.join(
    root,
    "backend/internal/buildinfo/version.go",
  );
  const backendVersionSource = fs.readFileSync(backendVersionPath, "utf8");
  fs.writeFileSync(
    backendVersionPath,
    replaceRequired(
      backendVersionSource,
      /^const Version = "[^"]+"$/m,
      `const Version = "${version}"`,
      "backend build version",
    ),
  );
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
    values.push([
      `${relativePath} pinned MCP config`,
      source.match(
        /@aipermission\/mcp@(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)/,
      )?.[1],
    ]);
  }

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
