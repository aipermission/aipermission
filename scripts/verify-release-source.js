#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

const root = path.resolve(__dirname, "..");
const requiredChecks = [
  "Security Hygiene",
  "Backend",
  "Frontend",
  "MCP Package",
  "MCP Windows Private Config",
  "Docs Hygiene",
  "NPM Placeholder Package",
  "Container Scan",
  "Analyze (go)",
  "Analyze (javascript-typescript)",
];
const advisoryWorkflows = [
  ["connector-conformance.yml", "Connector conformance"],
  ["native-dependency-freshness.yml", "Native dependency freshness"],
];
const releaseTagPattern = /^v(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$/;

function parseArguments(argv) {
  const values = {};
  for (let index = 0; index < argv.length; index += 2) {
    const key = argv[index];
    const value = argv[index + 1];
    if (!key?.startsWith("--") || !value) {
      throw new Error(
        "usage: verify-release-source.js --repository OWNER/REPO --sha SHA --tag vX.Y.Z",
      );
    }
    values[key.slice(2)] = value;
  }
  return values;
}

function releaseVersionFromTag(tag) {
  const match = releaseTagPattern.exec(tag || "");
  if (!match) throw new Error(`invalid release tag: ${tag || "missing"}`);
  return match[1];
}

function newestCheckByName(checkRuns) {
  const newest = new Map();
  for (const check of checkRuns || []) {
    if (check.app?.slug !== "github-actions") continue;
    const current = newest.get(check.name);
    if (!current || Number(check.id || 0) > Number(current.id || 0)) {
      newest.set(check.name, check);
    }
  }
  return newest;
}

function evaluateRequiredChecks(checkRuns, names = requiredChecks) {
  const checks = newestCheckByName(checkRuns);
  const pending = [];
  const failed = [];
  for (const name of names) {
    const check = checks.get(name);
    if (!check || check.status !== "completed") {
      pending.push(name);
      continue;
    }
    if (check.conclusion !== "success") {
      failed.push({ name, conclusion: check.conclusion || "unknown" });
    }
  }
  return { pending, failed };
}

function verifyLocalReleaseSource({ sha, tag }) {
  const version = releaseVersionFromTag(tag);
  const manifest = JSON.parse(
    fs.readFileSync(path.join(root, "release-manifest.json"), "utf8"),
  );
  if (manifest.version !== version) {
    throw new Error(
      `release tag ${tag} does not match release manifest ${manifest.version}`,
    );
  }
  const tagCommit = execFileSync(
    "git",
    ["rev-parse", `refs/tags/${tag}^{commit}`],
    {
      cwd: root,
      encoding: "utf8",
    },
  ).trim();
  const sourceCommit = execFileSync("git", ["rev-parse", `${sha}^{commit}`], {
    cwd: root,
    encoding: "utf8",
  }).trim();
  if (tagCommit !== sourceCommit) {
    throw new Error(
      `release tag ${tag} points to ${tagCommit}, not requested source ${sourceCommit}`,
    );
  }
  const mainCommit = execFileSync(
    "git",
    ["rev-parse", "refs/remotes/origin/main^{commit}"],
    {
      cwd: root,
      encoding: "utf8",
    },
  ).trim();
  try {
    execFileSync(
      "git",
      ["merge-base", "--is-ancestor", sourceCommit, mainCommit],
      {
        cwd: root,
        stdio: "ignore",
      },
    );
  } catch {
    throw new Error(
      `release source ${sourceCommit} is not reachable from origin/main at ${mainCommit}`,
    );
  }
  execFileSync(
    process.execPath,
    [path.join(root, "scripts/release-version.js"), "--check"],
    {
      cwd: root,
      stdio: "inherit",
    },
  );
  return sourceCommit;
}

async function githubJSON(url, token) {
  const response = await fetch(url, {
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "X-GitHub-Api-Version": "2022-11-28",
    },
  });
  if (!response.ok) {
    throw new Error(`GitHub API ${response.status} for ${url}`);
  }
  return response.json();
}

async function waitForRequiredChecks({
  apiURL,
  repository,
  sha,
  token,
  attempts = 60,
  delayMS = 10_000,
}) {
  const endpoint = `${apiURL}/repos/${repository}/commits/${sha}/check-runs?per_page=100`;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    const payload = await githubJSON(endpoint, token);
    const result = evaluateRequiredChecks(payload.check_runs);
    if (result.failed.length > 0) {
      throw new Error(
        result.failed
          .map((check) => `${check.name}: ${check.conclusion}`)
          .join(", "),
      );
    }
    if (result.pending.length === 0) return;
    if (attempt < attempts)
      await new Promise((resolve) => setTimeout(resolve, delayMS));
  }
  throw new Error(`timed out waiting for required checks on ${sha}`);
}

async function reportAdvisorySignals({
  apiURL,
  repository,
  token,
  maxAgeDays = 14,
}) {
  const now = Date.now();
  for (const [workflow, label] of advisoryWorkflows) {
    try {
      const endpoint = `${apiURL}/repos/${repository}/actions/workflows/${workflow}/runs?branch=main&status=completed&per_page=1`;
      const payload = await githubJSON(endpoint, token);
      const run = payload.workflow_runs?.[0];
      const ageDays = run?.created_at
        ? (now - Date.parse(run.created_at)) / 86_400_000
        : Number.POSITIVE_INFINITY;
      if (!run || run.conclusion !== "success" || ageDays > maxAgeDays) {
        const state = run
          ? `${run.conclusion || "unknown"}, ${Math.floor(ageDays)} day(s) old`
          : "missing";
        console.log(
          `::warning::${label} is advisory and needs maintainer review (${state}).`,
        );
      } else {
        console.log(
          `${label} signal is ${Math.floor(ageDays)} day(s) old and successful.`,
        );
      }
    } catch (error) {
      console.log(
        `::warning::Could not read advisory ${label} signal: ${error.message}`,
      );
    }
  }
}

async function main() {
  const args = parseArguments(process.argv.slice(2));
  const repository = args.repository || process.env.GITHUB_REPOSITORY;
  const sha = args.sha || process.env.GITHUB_SHA;
  const tag = args.tag || process.env.GITHUB_REF_NAME;
  const token = process.env.GITHUB_TOKEN;
  const apiURL = process.env.GITHUB_API_URL || "https://api.github.com";
  if (!repository || !sha || !tag || !token) {
    throw new Error("repository, sha, tag, and GITHUB_TOKEN are required");
  }
  const sourceCommit = verifyLocalReleaseSource({ sha, tag });
  await waitForRequiredChecks({ apiURL, repository, sha: sourceCommit, token });
  await reportAdvisorySignals({ apiURL, repository, token });
  console.log(`Release source ${tag} verified at ${sourceCommit}.`);
}

if (require.main === module) {
  main().catch((error) => {
    console.error(`Release source verification failed: ${error.message}`);
    process.exit(1);
  });
}

module.exports = {
  evaluateRequiredChecks,
  newestCheckByName,
  releaseVersionFromTag,
  requiredChecks,
};
