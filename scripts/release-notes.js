#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const sourcePath = path.join(root, "release-notes.json");
const changelogPath = path.join(root, "CHANGELOG.md");
const frontendPath = path.join(root, "frontend/src/lib/release.generated.json");
const manifestPath = path.join(root, "release-manifest.json");
const semverPattern = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/;
const datePattern = /^\d{4}-\d{2}-\d{2}$/;

function readJSON(filePath) {
  return JSON.parse(fs.readFileSync(filePath, "utf8"));
}

function validateSections(sections, label) {
  if (!Array.isArray(sections)) {
    throw new Error(`${label} sections must be an array`);
  }
  for (const [sectionIndex, section] of sections.entries()) {
    if (
      !section ||
      typeof section.title !== "string" ||
      !section.title.trim()
    ) {
      throw new Error(`${label} section ${sectionIndex + 1} requires a title`);
    }
    if (!Array.isArray(section.items) || section.items.length === 0) {
      throw new Error(`${label} section ${section.title} requires items`);
    }
    for (const [itemIndex, item] of section.items.entries()) {
      if (typeof item !== "string" || !item.trim() || item.includes("\n")) {
        throw new Error(
          `${label} section ${section.title} item ${itemIndex + 1} must be one non-empty line`,
        );
      }
    }
  }
}

function compareSemver(left, right) {
  const leftSeparator = left.indexOf("-");
  const rightSeparator = right.indexOf("-");
  const leftCore = leftSeparator === -1 ? left : left.slice(0, leftSeparator);
  const rightCore =
    rightSeparator === -1 ? right : right.slice(0, rightSeparator);
  const leftPrerelease =
    leftSeparator === -1 ? undefined : left.slice(leftSeparator + 1);
  const rightPrerelease =
    rightSeparator === -1 ? undefined : right.slice(rightSeparator + 1);
  const leftParts = leftCore.split(".").map(Number);
  const rightParts = rightCore.split(".").map(Number);
  for (let index = 0; index < 3; index += 1) {
    if (leftParts[index] !== rightParts[index]) {
      return leftParts[index] - rightParts[index];
    }
  }
  if (leftPrerelease === undefined || rightPrerelease === undefined) {
    if (leftPrerelease === rightPrerelease) return 0;
    return leftPrerelease === undefined ? 1 : -1;
  }
  const leftIdentifiers = leftPrerelease.split(".");
  const rightIdentifiers = rightPrerelease.split(".");
  const length = Math.max(leftIdentifiers.length, rightIdentifiers.length);
  for (let index = 0; index < length; index += 1) {
    if (leftIdentifiers[index] === undefined) return -1;
    if (rightIdentifiers[index] === undefined) return 1;
    if (leftIdentifiers[index] === rightIdentifiers[index]) continue;
    const leftNumeric = /^\d+$/.test(leftIdentifiers[index]);
    const rightNumeric = /^\d+$/.test(rightIdentifiers[index]);
    if (leftNumeric && rightNumeric) {
      return Number(leftIdentifiers[index]) - Number(rightIdentifiers[index]);
    }
    if (leftNumeric !== rightNumeric) return leftNumeric ? -1 : 1;
    return leftIdentifiers[index].localeCompare(rightIdentifiers[index]);
  }
  return 0;
}

function validateSource(source, currentVersion) {
  if (
    !source ||
    !Array.isArray(source.releases) ||
    source.releases.length === 0
  ) {
    throw new Error("release-notes.json requires at least one release");
  }
  if (
    !Number.isInteger(source.inAppReleaseLimit) ||
    source.inAppReleaseLimit < 1
  ) {
    throw new Error("inAppReleaseLimit must be a positive integer");
  }
  validateSections(source.unreleased || [], "unreleased");

  const seen = new Set();
  for (const [index, release] of source.releases.entries()) {
    const label = `release ${index + 1}`;
    if (!release || !semverPattern.test(release.version || "")) {
      throw new Error(`${label} has an invalid version`);
    }
    if (seen.has(release.version)) {
      throw new Error(`duplicate release version ${release.version}`);
    }
    seen.add(release.version);
    if (!datePattern.test(release.date || "")) {
      throw new Error(`${label} has an invalid date`);
    }
    if (typeof release.label !== "string" || !release.label.trim()) {
      throw new Error(`${label} requires an in-app label`);
    }
    validateSections(release.sections, label);
    if (
      index > 0 &&
      compareSemver(source.releases[index - 1].version, release.version) <= 0
    ) {
      throw new Error("releases must be ordered newest to oldest");
    }
  }
  if (source.releases[0].version !== currentVersion) {
    throw new Error(
      `latest release ${source.releases[0].version} does not match release manifest ${currentVersion}`,
    );
  }
}

function wrapBullet(item, width = 88) {
  const words = item.trim().split(/\s+/);
  const lines = [];
  let line = "-";
  for (const word of words) {
    const candidate = line === "-" ? `- ${word}` : `${line} ${word}`;
    if (candidate.length <= width || line === "-") {
      line = candidate;
      continue;
    }
    lines.push(line);
    line = `  ${word}`;
  }
  lines.push(line);
  return lines.join("\n");
}

function renderSections(sections) {
  if (sections.length === 0) {
    return "";
  }
  return sections
    .map(
      (section) =>
        `### ${section.title}\n\n${section.items.map((item) => wrapBullet(item)).join("\n")}`,
    )
    .join("\n\n");
}

function renderChangelog(source) {
  const introduction = [
    "# Changelog",
    "",
    "<!-- Generated from release-notes.json. Do not edit directly. -->",
    "",
    "All notable changes to this project will be documented in this file.",
    "",
    "The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),",
    "and this project uses semantic versioning for public releases.",
    "",
    "## [Unreleased]",
  ];
  const unreleased = renderSections(source.unreleased || []);
  if (unreleased) {
    introduction.push("", unreleased);
  }
  const releases = source.releases.map((release) => {
    return `## [${release.version}] - ${release.date}\n\n${renderSections(release.sections)}`;
  });
  return `${introduction.join("\n")}\n\n${releases.join("\n\n")}\n`;
}

function renderFrontend(source, currentVersion) {
  const entries = source.releases
    .slice(0, source.inAppReleaseLimit)
    .map(({ version, label, sections }) => ({ version, label, sections }));
  return `${JSON.stringify({ version: currentVersion, entries }, null, 2)}\n`;
}

function generatedArtifacts() {
  const source = readJSON(sourcePath);
  const currentVersion = readJSON(manifestPath).version;
  validateSource(source, currentVersion);
  return [
    [changelogPath, renderChangelog(source)],
    [frontendPath, renderFrontend(source, currentVersion)],
  ];
}

function writeArtifacts() {
  for (const [filePath, content] of generatedArtifacts()) {
    fs.writeFileSync(filePath, content);
  }
}

function checkArtifacts() {
  const stale = [];
  for (const [filePath, expected] of generatedArtifacts()) {
    const current = fs.readFileSync(filePath, "utf8");
    if (current !== expected) {
      stale.push(path.relative(root, filePath));
    }
  }
  if (stale.length > 0) {
    throw new Error(`${stale.join(", ")} stale; run npm run release-notes`);
  }
  console.log("Generated release notes are current.");
}

if (require.main === module) {
  try {
    if (process.argv[2] === "--check") {
      checkArtifacts();
    } else if (process.argv.length > 2) {
      throw new Error("usage: release-notes.js [--check]");
    } else {
      writeArtifacts();
      console.log("Generated CHANGELOG.md and the frontend release artifact.");
    }
  } catch (error) {
    console.error(`Release notes generation failed: ${error.message}`);
    process.exit(1);
  }
}

module.exports = {
  renderChangelog,
  renderFrontend,
  compareSemver,
  validateSource,
  wrapBullet,
};
