const assert = require("node:assert/strict");
const test = require("node:test");

const {
  evaluateRequiredChecks,
  newestCheckByName,
  releaseVersionFromTag,
  requiredChecks,
} = require("./verify-release-source");

test("release source requires the Windows private-config security check", () => {
  assert.ok(requiredChecks.includes("MCP Windows Private Config"));
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
