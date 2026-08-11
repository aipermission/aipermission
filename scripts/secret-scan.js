#!/usr/bin/env node
const { execFileSync } = require("node:child_process");
const { readFileSync } = require("node:fs");
const path = require("node:path");

const tracked = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" })
  .split("\0")
  .filter(Boolean);

const skippedPathParts = new Set(["node_modules", "dist", "coverage", "test-results", ".git"]);
const fixturePatterns = [
  /(^|\/)(test|tests|e2e)\//,
  /_test\.go$/,
  /\.test\.(js|jsx|ts|tsx)$/,
  /(^|\/)docs\/api\/rest-api\.md$/,
  /(^|\/)docs\/security\/threat-model\.md$/,
];
const allowedEncryptedFixtures = new Set([
  "backend/internal/db/testdata/aipermission-schema13-sqlcipher-4.4.2.aipdb",
  "backend/internal/db/testdata/sqlcipher-4.4.2.aipdb",
]);
const blockedTrackedNames = [
  /^\.env$/,
  /^\.env\.(?!example$).+/,
  /(^|\/)gateway\.secret$/,
  /(^|\/)known_hosts$/,
  /(^|\/)id_(rsa|ed25519)(\.pub)?$/,
  /\.(aipdb|aipbackup|pem|key|p12|pfx)$/,
];
const secretPatterns = [
  /-----BEGIN [A-Z ]*PRIVATE KEY-----/,
  /\bAKIA[0-9A-Z]{16}\b/,
  /\bghp_[A-Za-z0-9_]{36,}\b/,
  /\bgithub_pat_[A-Za-z0-9_]{40,}\b/,
  /\bsk-[A-Za-z0-9]{32,}\b/,
];

function isSkipped(file) {
  const parts = file.split(/[\\/]/);
  if (parts.some((part) => skippedPathParts.has(part))) return true;
  return fixturePatterns.some((pattern) => pattern.test(file));
}

const findings = [];
for (const file of tracked) {
  const normalized = file.split(path.sep).join("/");
  if (allowedEncryptedFixtures.has(normalized)) continue;
  if (blockedTrackedNames.some((pattern) => pattern.test(normalized))) {
    findings.push(`${file}: tracked local secret-like file name`);
    continue;
  }
  if (isSkipped(normalized)) continue;
  let content;
  try {
    content = readFileSync(file, "utf8");
  } catch {
    continue;
  }
  for (const pattern of secretPatterns) {
    if (pattern.test(content)) {
      findings.push(`${file}: matched ${pattern}`);
      break;
    }
  }
}

if (findings.length > 0) {
  console.error("Secret scan failed:");
  for (const finding of findings) console.error(`- ${finding}`);
  process.exit(1);
}

console.log(`Secret scan passed (${tracked.length} tracked files checked).`);
