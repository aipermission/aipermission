const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const { parseDocument } = require("yaml");

const {
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
} = require("./verify-release-source");

const root = path.resolve(__dirname, "..");

function workflowSteps(relativePath, jobID) {
  const source = fs.readFileSync(path.join(root, relativePath), "utf8");
  const document = parseDocument(source, {
    maxAliasCount: 0,
    strict: true,
    uniqueKeys: true,
  });
  assert.deepEqual(document.errors, []);
  const workflow = document.toJS({ maxAliasCount: 0 });
  return workflow.jobs[jobID].steps;
}

test("publish workflows install locked verifier dependencies before verification", () => {
  for (const [workflow, job] of [
    [".github/workflows/publish-images.yml", "verify-source"],
    [".github/workflows/publish-mcp.yml", "publish"],
  ]) {
    const steps = workflowSteps(workflow, job);
    const install = steps.findIndex(
      (step) => step.run === "npm ci --prefix scripts --workspaces=false",
    );
    const verify = steps.findIndex((step) =>
      step.run?.includes("node scripts/verify-release-source.js"),
    );
    assert.ok(install >= 0, `${workflow} must install verifier dependencies`);
    assert.ok(verify > install, `${workflow} must install dependencies before verification`);
  }
});

test("release source requires the Windows private-config security check", () => {
  assert.ok(requiredChecks.includes("MCP Windows Private Config"));
});

test("release source requires real-service connector conformance on the same commit", () => {
  assert.ok(
    requiredChecks.includes("ClickHouse, Postgres, Valkey, RabbitMQ, and S3"),
  );
});

test("every release check is bound to one reviewed workflow", () => {
  assert.deepEqual([...requiredWorkflowByCheck.keys()], requiredChecks);
  assert.deepEqual([...requiredJobNameByCheck.keys()], requiredChecks);
});

test("releaseVersionFromTag accepts release and prerelease tags", () => {
  assert.equal(releaseVersionFromTag("v0.2.30"), "0.2.30");
  assert.equal(releaseVersionFromTag("v0.2.30-rc.1"), "0.2.30-rc.1");
  assert.throws(() => releaseVersionFromTag("0.2.30"), /invalid release tag/);
  assert.throws(() => releaseVersionFromTag("vnext"), /invalid release tag/);
});

test("newestCheckByName uses the latest rerun", () => {
  const checks = newestCheckByName([
    {
      id: 10,
      name: "Backend",
      status: "completed",
      conclusion: "failure",
      app: { slug: "github-actions" },
    },
    {
      id: 12,
      name: "Backend",
      status: "completed",
      conclusion: "success",
      app: { slug: "github-actions" },
    },
    {
      id: 14,
      name: "Backend",
      status: "completed",
      conclusion: "success",
      app: { slug: "other-app" },
    },
  ]);
  assert.equal(checks.get("Backend").conclusion, "success");
});

test("evaluateRequiredChecks separates pending and failed checks", () => {
  assert.deepEqual(
    evaluateRequiredChecks(
      [
        {
          id: 1,
          name: "Backend",
          status: "completed",
          conclusion: "success",
          app: { slug: "github-actions" },
        },
        {
          id: 2,
          name: "Frontend",
          status: "in_progress",
          conclusion: null,
          app: { slug: "github-actions" },
        },
        {
          id: 3,
          name: "CodeQL",
          status: "completed",
          conclusion: "failure",
          app: { slug: "github-actions" },
        },
      ],
      ["Backend", "Frontend", "CodeQL", "Missing"],
    ),
    {
      pending: ["Frontend", "Missing"],
      failed: [{ name: "CodeQL", conclusion: "failure" }],
    },
  );
});

test("release checks require exact push workflow provenance", () => {
  const now = Date.parse("2026-09-13T01:00:00Z");
  const recent = "2026-09-13T00:30:00Z";
  const check = (id, detailsURL) => ({
    id,
    name: "Backend",
    status: "completed",
    conclusion: "success",
    app: { slug: "github-actions" },
    details_url: detailsURL,
    started_at: recent,
    completed_at: recent,
  });
  const run = (id, overrides = {}) => ({
    id,
    path: ".github/workflows/ci.yml",
    event: "push",
    head_branch: "main",
    head_sha: "release-sha",
    run_number: id,
    run_attempt: 1,
    status: "completed",
    conclusion: "success",
    created_at: recent,
    updated_at: recent,
    ...overrides,
  });
  const checks = [
    check(1, "https://github.com/org/repo/actions/runs/10/job/1"),
    check(2, "https://github.com/org/repo/actions/runs/11/job/2"),
    check(3, "https://github.com/org/repo/actions/runs/12/job/3"),
    check(4, "https://github.com/org/repo/actions/runs/13/job/4"),
    check(5, "https://github.com/org/repo/actions/runs/14/job/5"),
  ];
  const runs = [
    run(10),
    run(11, { event: "pull_request" }),
    run(12, { head_branch: "dev" }),
    run(13, { head_sha: "other-sha" }),
    run(14, { path: ".github/workflows/untrusted.yml" }),
  ];
  const jobs = checks.map((candidate, index) => ({
    id: index + 1,
    run_id: 10 + index,
    name: "Backend",
    run_attempt: 1,
    status: "completed",
    conclusion: "success",
    started_at: recent,
    completed_at: recent,
    check_run_url: `https://api.github.com/repos/org/repo/check-runs/${candidate.id}`,
  }));
  assert.deepEqual(verifiedRequiredCheckRuns(checks, runs, jobs, "release-sha", { now }), [checks[0]]);
  assert.deepEqual(
    verifiedRequiredCheckRuns(checks, runs, [{ ...jobs[0], name: "Decoy" }], "release-sha", { now }),
    [],
  );
});

function provenanceFixture(overrides = {}) {
  const timestamp = overrides.timestamp === undefined ? "2026-09-13T00:30:00Z" : overrides.timestamp;
  const runID = overrides.runID || 10;
  const checkID = overrides.checkID || 20;
  const jobID = overrides.jobID || checkID + 10;
  return {
    run: {
      id: runID,
      path: ".github/workflows/ci.yml",
      event: "push",
      head_branch: "main",
      head_sha: "release-sha",
      run_number: overrides.runNumber || runID,
      run_attempt: overrides.runAttempt || 1,
      status: overrides.status || "completed",
      conclusion: overrides.conclusion === undefined ? "success" : overrides.conclusion,
      created_at: timestamp,
      updated_at: timestamp,
    },
    check: {
      id: checkID,
      name: "Backend",
      status: "completed",
      conclusion: "success",
      app: { slug: "github-actions" },
      details_url: `https://github.com/org/repo/actions/runs/${runID}/job/${jobID}`,
      started_at: timestamp,
      completed_at: timestamp,
    },
    job: {
      id: jobID,
      run_id: runID,
      run_attempt: overrides.runAttempt || 1,
      name: "Backend",
      status: "completed",
      conclusion: "success",
      check_run_url: `https://api.github.com/repos/org/repo/check-runs/${checkID}`,
      started_at: timestamp,
      completed_at: timestamp,
    },
  };
}

test("newest exact-SHA workflow run cannot fall back to an older success", () => {
  const now = Date.parse("2026-09-13T01:00:00Z");
  const old = provenanceFixture({ runID: 10, runNumber: 10, checkID: 20 });
  const canceled = provenanceFixture({
    runID: 11,
    runNumber: 11,
    checkID: 21,
    conclusion: "cancelled",
  });
  const selected = selectRequiredWorkflowRuns([old.run, canceled.run], "release-sha");
  assert.equal(selected.get(".github/workflows/ci.yml").id, 11);
  assert.deepEqual(
    verifiedRequiredCheckRuns(
      [old.check],
      [old.run, canceled.run],
      [old.job],
      "release-sha",
      { now },
    ),
    [],
  );
  assert.deepEqual(
    evaluateRequiredWorkflowRuns([old.run, canceled.run], "release-sha", { now }).failed,
    [{ workflow: ".github/workflows/ci.yml", conclusion: "cancelled" }],
  );
});

test("latest successful rerun is the only accepted attempt", () => {
  const now = Date.parse("2026-09-13T01:00:00Z");
  const first = provenanceFixture({ runID: 10, runNumber: 10, runAttempt: 1, checkID: 20 });
  const rerun = provenanceFixture({ runID: 10, runNumber: 10, runAttempt: 2, checkID: 21 });
  assert.deepEqual(
    verifiedRequiredCheckRuns(
      [first.check, rerun.check],
      [first.run, rerun.run],
      [first.job, rerun.job],
      "release-sha",
      { now },
    ),
    [rerun.check],
  );
});

test("release evidence rejects stale, missing, and future timestamps", () => {
  const now = Date.parse("2026-09-13T01:00:00Z");
  for (const timestamp of [
    new Date(now - requiredCheckMaxAgeMS - 1).toISOString(),
    "",
    new Date(now + 6 * 60 * 1000).toISOString(),
  ]) {
    const fixture = provenanceFixture({ timestamp });
    assert.deepEqual(
      verifiedRequiredCheckRuns(
        [fixture.check],
        [fixture.run],
        [fixture.job],
        "release-sha",
        { now },
      ),
      [],
      `timestamp ${timestamp || "missing"} should be rejected`,
    );
  }
});

test("release evidence binds jobs to the selected run attempt", () => {
  const now = Date.parse("2026-09-13T01:00:00Z");
  const fixture = provenanceFixture({ runAttempt: 2 });
  fixture.job.run_attempt = 1;
  assert.deepEqual(
    verifiedRequiredCheckRuns(
      [fixture.check],
      [fixture.run],
      [fixture.job],
      "release-sha",
      { now },
    ),
    [],
  );
});
