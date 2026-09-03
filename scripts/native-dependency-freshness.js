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

async function resolveTagCommit(repository, tag) {
  const reference = await githubJSON(
    `https://api.github.com/repos/${repository}/git/ref/tags/${encodeURIComponent(tag)}`,
  );
  let object = reference.object;
  if (object?.type === "tag") {
    object = (
      await githubJSON(
        `https://api.github.com/repos/${repository}/git/tags/${object.sha}`,
      )
    ).object;
  }
  if (object?.type !== "commit" || !/^[0-9a-f]{40}$/i.test(object.sha || "")) {
    throw new Error(`could not resolve ${repository} ${tag} to a commit`);
  }
  return object.sha;
}

async function main() {
  if (!/^[0-9a-f]{40}$/i.test(String(inventory.wrapper_commit || ""))) {
    throw new Error(
      "native dependency inventory wrapper_commit must be a full Git commit",
    );
  }
  if (
    !/^\d+\.\d+\.\d+$/.test(
      String(inventory.reviewed_upstream_runtime_version || ""),
    )
  ) {
    throw new Error(
      "native dependency inventory reviewed_upstream_runtime_version must be a semantic version",
    );
  }
  const [wrapper, release, reviewedUpstreamCommit] = await Promise.all([
    githubJSON(
      `https://api.github.com/repos/${inventory.wrapper_repository}/commits/${inventory.wrapper_branch}`,
    ),
    githubJSON(
      `https://api.github.com/repos/${inventory.upstream_repository}/releases/latest`,
    ),
    resolveTagCommit(
      inventory.upstream_repository,
      inventory.reviewed_upstream_tag,
    ),
  ]);

  const latestRuntime = String(release.tag_name || "").replace(/^v/, "");
  const failures = [];
  if (String(wrapper.sha || "") !== inventory.wrapper_commit) {
    failures.push(
      `SQLCipher wrapper advanced from ${inventory.wrapper_commit} to ${wrapper.sha || "unknown"}`,
    );
  }
  if (latestRuntime !== inventory.reviewed_upstream_runtime_version) {
    failures.push(
      `upstream SQLCipher advanced from reviewed ${inventory.reviewed_upstream_runtime_version} to ${latestRuntime || "unknown"}`,
    );
  }
  if (reviewedUpstreamCommit !== inventory.reviewed_upstream_commit) {
    failures.push(
      `reviewed SQLCipher tag ${inventory.reviewed_upstream_tag} resolves to ${reviewedUpstreamCommit}, not inventoried ${inventory.reviewed_upstream_commit}`,
    );
  }
  const reviewDate = new Date(`${inventory.reviewed_upstream_date}T00:00:00Z`);
  const reviewAgeDays = Math.floor(
    (Date.now() - reviewDate.getTime()) / 86_400_000,
  );
  if (
    !Number.isInteger(inventory.review_max_age_days) ||
    Number.isNaN(reviewDate.getTime())
  ) {
    failures.push("native dependency review date or maximum age is invalid");
  } else if (reviewAgeDays < 0) {
    failures.push("native dependency review date must not be in the future");
  } else if (reviewAgeDays > inventory.review_max_age_days) {
    failures.push(
      `native dependency review is ${reviewAgeDays} days old; maximum age is ${inventory.review_max_age_days} days`,
    );
  }

  if (failures.length > 0) {
    console.error("Native dependency freshness review required:");
    for (const failure of failures) console.error(`- ${failure}`);
    process.exit(1);
  }

  console.log(
    `Native dependency sources unchanged: wrapper ${inventory.wrapper_commit}; reviewed SQLCipher ${latestRuntime} at ${reviewedUpstreamCommit}.`,
  );
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
});
