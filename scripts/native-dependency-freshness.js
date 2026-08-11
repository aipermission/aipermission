#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
const inventory = JSON.parse(
  fs.readFileSync(
    path.join(root, "docs/security/native-dependencies.json"),
    "utf8",
  ),
).sqlcipher;

const headers = {
  Accept: "application/vnd.github+json",
  "User-Agent": "aipermission-native-dependency-check",
  "X-GitHub-Api-Version": "2022-11-28",
};
if (process.env.GITHUB_TOKEN) {
  headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
}

async function githubJSON(url) {
  const response = await fetch(url, { headers });
  if (!response.ok) {
    throw new Error(`GitHub API ${response.status} for ${url}`);
  }
  return response.json();
}

async function main() {
  const [wrapper, release] = await Promise.all([
    githubJSON(
      "https://api.github.com/repos/SE-I-T-Digital/go-sqlcipher/commits/main",
    ),
    githubJSON("https://api.github.com/repos/sqlcipher/sqlcipher/releases/latest"),
  ]);

  const latestRuntime = String(release.tag_name || "").replace(/^v/, "");
  const failures = [];
  if (!String(wrapper.sha || "").startsWith(inventory.wrapper_commit)) {
    failures.push(
      `SQLCipher wrapper advanced from ${inventory.wrapper_commit} to ${wrapper.sha || "unknown"}`,
    );
  }
  if (latestRuntime !== inventory.reviewed_upstream_runtime_version) {
    failures.push(
      `upstream SQLCipher advanced from reviewed ${inventory.reviewed_upstream_runtime_version} to ${latestRuntime || "unknown"}`,
    );
  }

  if (failures.length > 0) {
    console.error("Native dependency freshness review required:");
    for (const failure of failures) console.error(`- ${failure}`);
    process.exit(1);
  }

  console.log(
    `Native dependency sources unchanged: wrapper ${inventory.wrapper_commit}; reviewed SQLCipher ${latestRuntime}.`,
  );
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
});
