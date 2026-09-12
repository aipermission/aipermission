const assert = require("node:assert/strict");
const test = require("node:test");

const {
  evaluateRequiredChecks,
  newestCheckByName,
  releaseVersionFromTag,
  requiredChecks,
  requiredWorkflowByCheck,
  verifiedRequiredCheckRuns,
} = require("./verify-release-source");

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
  const check = (id, detailsURL) => ({
    id,
    name: "Backend",
    status: "completed",
    conclusion: "success",
    app: { slug: "github-actions" },
    details_url: detailsURL,
  });
  const run = (id, overrides = {}) => ({
    id,
    path: ".github/workflows/ci.yml",
    event: "push",
    head_branch: "main",
    head_sha: "release-sha",
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
  assert.deepEqual(verifiedRequiredCheckRuns(checks, runs, "release-sha"), [
    checks[0],
  ]);
});
