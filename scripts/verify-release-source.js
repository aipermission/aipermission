#!/usr/bin/env node

const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");
const { loadPolicy, verifyWorkflows } = require("./verification-policy");

const root = path.resolve(__dirname, "..");
const requiredGates = loadPolicy().required_checks;
const requiredChecks = requiredGates.map((gate) => gate.name);
const requiredWorkflowByCheck = new Map(
  requiredGates.map((gate) => [gate.name, gate.workflow]),
);
const requiredJobNameByCheck = new Map(
  requiredGates.map((gate) => [gate.name, gate.job_name]),
);
const advisoryWorkflows = [
  ["native-dependency-freshness.yml", "Native dependency freshness"],
];
const releaseTagPattern = /^v(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)$/;
const requiredCheckMaxAgeMS = 7 * 24 * 60 * 60 * 1000;
const allowedClockSkewMS = 5 * 60 * 1000;

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

function actionRunID(detailsURL) {
  const match = /\/actions\/runs\/(\d+)(?:\/|$)/.exec(detailsURL || "");
  return match?.[1] || "";
}

function actionJobID(detailsURL) {
  const match = /\/actions\/runs\/\d+\/job\/(\d+)(?:\/|$)/.exec(detailsURL || "");
  return match?.[1] || "";
}

function requiredWorkflowPaths() {
  return new Set(requiredWorkflowByCheck.values());
}

function runRecency(run) {
  const runNumber = Number(run?.run_number);
  const id = Number(run?.id);
  const attempt = Number(run?.run_attempt);
  return [
    Number.isFinite(runNumber) ? runNumber : Number.isFinite(id) ? id : -1,
    Number.isFinite(attempt) ? attempt : 1,
    Number.isFinite(id) ? id : -1,
  ];
}

function isNewerRun(candidate, current) {
  const left = runRecency(candidate);
  const right = runRecency(current);
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) return left[index] > right[index];
  }
  return false;
}

function validEvidenceTime(value, now, maxAgeMS) {
  const timestamp = Date.parse(value || "");
  return (
    Number.isFinite(timestamp) &&
    timestamp >= now - maxAgeMS &&
    timestamp <= now + allowedClockSkewMS
  );
}

function validRunEvidence(run, now, maxAgeMS) {
  return (
    validEvidenceTime(run?.created_at, now, maxAgeMS) &&
    validEvidenceTime(run?.updated_at, now, maxAgeMS)
  );
}

function validCheckEvidence(check, now, maxAgeMS) {
  return (
    validEvidenceTime(check?.started_at, now, maxAgeMS) &&
    validEvidenceTime(check?.completed_at, now, maxAgeMS)
  );
}

function validJobEvidence(job, now, maxAgeMS) {
  return (
    validEvidenceTime(job?.started_at, now, maxAgeMS) &&
    validEvidenceTime(job?.completed_at, now, maxAgeMS)
  );
}

function selectRequiredWorkflowRuns(workflowRuns, sha) {
  const expected = requiredWorkflowPaths();
  const selected = new Map();
  for (const run of workflowRuns || []) {
    if (
      !expected.has(run?.path) ||
      run.event !== "push" ||
      run.head_branch !== "main" ||
      run.head_sha !== sha
    ) {
      continue;
    }
    const current = selected.get(run.path);
    if (!current || isNewerRun(run, current)) selected.set(run.path, run);
  }
  return selected;
}

function evaluateRequiredWorkflowRuns(
  workflowRuns,
  sha,
  { now = Date.now(), maxAgeMS = requiredCheckMaxAgeMS } = {},
) {
  const selected = selectRequiredWorkflowRuns(workflowRuns, sha);
  const pending = [];
  const failed = [];
  for (const workflow of requiredWorkflowPaths()) {
    const run = selected.get(workflow);
    if (!run || run.status !== "completed") {
      pending.push(workflow);
      continue;
    }
    if (run.conclusion !== "success") {
      failed.push({ workflow, conclusion: run.conclusion || "unknown" });
      continue;
    }
    if (!validRunEvidence(run, now, maxAgeMS)) {
      failed.push({ workflow, conclusion: "stale_or_invalid_evidence" });
    }
  }
  return { selected, pending, failed };
}

function verifiedRequiredCheckRuns(
  checkRuns,
  workflowRuns,
  jobs,
  sha,
  options = {},
) {
  const now = options.now ?? Date.now();
  const maxAgeMS = options.maxAgeMS ?? requiredCheckMaxAgeMS;
  const assessment = evaluateRequiredWorkflowRuns(workflowRuns, sha, {
    now,
    maxAgeMS,
  });
  const runs = new Map(
    [...assessment.selected.values()].map((run) => [String(run.id), run]),
  );
  const jobsByID = new Map((jobs || []).map((job) => [String(job.id), job]));
  return (checkRuns || []).filter((check) => {
    const expectedWorkflow = requiredWorkflowByCheck.get(check.name);
    if (!expectedWorkflow || check.app?.slug !== "github-actions") return false;
    const run = runs.get(actionRunID(check.details_url));
    const job = jobsByID.get(actionJobID(check.details_url));
    return (
      run?.path === expectedWorkflow &&
      run.status === "completed" &&
      run.conclusion === "success" &&
      validRunEvidence(run, now, maxAgeMS) &&
      String(job?.run_id) === String(run.id) &&
      Number(job?.run_attempt || 1) === Number(run.run_attempt || 1) &&
      job?.name === requiredJobNameByCheck.get(check.name) &&
      job.status === "completed" &&
      job.conclusion === "success" &&
      String(job?.check_run_url || "").endsWith(`/check-runs/${check.id}`) &&
      validJobEvidence(job, now, maxAgeMS) &&
      check.status === "completed" &&
      check.conclusion === "success" &&
      validCheckEvidence(check, now, maxAgeMS)
    );
  });
}

function verifyLocalReleaseSource({ sha, tag }) {
  verifyWorkflows();
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
  const checksEndpoint = `${apiURL}/repos/${repository}/commits/${sha}/check-runs?per_page=100`;
  const runsEndpoint = `${apiURL}/repos/${repository}/actions/runs?head_sha=${encodeURIComponent(sha)}&branch=main&event=push&per_page=100`;
  for (let attempt = 1; attempt <= attempts; attempt += 1) {
    const [checksPayload, runsPayload] = await Promise.all([
      githubJSON(checksEndpoint, token),
      githubJSON(runsEndpoint, token),
    ]);
    const runAssessment = evaluateRequiredWorkflowRuns(
      runsPayload.workflow_runs,
      sha,
    );
    if (runAssessment.failed.length > 0) {
      throw new Error(
        runAssessment.failed
          .map((run) => `${run.workflow}: ${run.conclusion}`)
          .join(", "),
      );
    }
    const candidateRuns = [...runAssessment.selected.values()];
    const jobs = (
      await Promise.all(
        candidateRuns.map(async (run) => {
          const payload = await githubJSON(
            `${apiURL}/repos/${repository}/actions/runs/${run.id}/jobs?per_page=100`,
            token,
          );
          return (payload.jobs || []).map((job) => ({
            ...job,
            run_id: run.id,
            run_attempt: job.run_attempt || run.run_attempt || 1,
          }));
        }),
      )
    ).flat();
    const verified = verifiedRequiredCheckRuns(
      checksPayload.check_runs,
      runsPayload.workflow_runs,
      jobs,
      sha,
    );
    const result = evaluateRequiredChecks(verified);
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
  evaluateRequiredWorkflowRuns,
  evaluateRequiredChecks,
  newestCheckByName,
  releaseVersionFromTag,
  requiredChecks,
  requiredJobNameByCheck,
  requiredWorkflowByCheck,
  requiredCheckMaxAgeMS,
  selectRequiredWorkflowRuns,
  verifiedRequiredCheckRuns,
};
