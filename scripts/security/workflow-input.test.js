const assert = require("node:assert/strict");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { spawnSync } = require("node:child_process");
const test = require("node:test");
const { parseYAMLMapping } = require("../workflow-contracts");
const { releaseVersionFromTag } = require("../verify-release-source");

const root = path.resolve(__dirname, "../..");
const workflowPath = ".github/workflows/publish-mcp.yml";
const workflow = parseYAMLMapping(
  fs.readFileSync(path.join(root, workflowPath), "utf8"),
  workflowPath,
);

test("MCP release tag is validated as environment data before checkout", (t) => {
  const job = workflow.jobs.publish;
  const first = job.steps[0];
  assert.equal(first.name, "Validate release tag");
  assert.equal(first.env.RELEASE_TAG, "${{ inputs.release_tag }}");
  assert.equal(first.shell, "bash");
  assert.equal(first.if, undefined);
  assert.equal(first["continue-on-error"], undefined);
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "aip-release-input-"),
  );
  t.after(() => fs.rmSync(directory, { recursive: true, force: true }));
  const tags = [
    "v0.2.76",
    "v1.2.3-rc.1",
    "v1.2.3$(touch marker)",
    "v1.2.3`touch marker`",
    'v1.2.3";touch marker;#',
    "v1.2.3\n",
    "--help",
    "",
    "v1.2.3/branch",
  ];
  for (const tag of tags) {
    const valid = ["v0.2.76", "v1.2.3-rc.1"].includes(tag);
    if (valid) assert.doesNotThrow(() => releaseVersionFromTag(tag));
    else assert.throws(() => releaseVersionFromTag(tag), /invalid release tag/);
    const result = spawnSync(
      "bash",
      ["--noprofile", "--norc", "-eo", "pipefail", "-c", first.run],
      {
        cwd: directory,
        env: { PATH: process.env.PATH, RELEASE_TAG: tag },
        encoding: "utf8",
        timeout: 3000,
      },
    );
    assert.equal(
      result.status,
      valid ? 0 : 1,
      `tag ${JSON.stringify(tag)}: ${result.stderr}`,
    );
    assert.equal(fs.existsSync(path.join(directory, "marker")), false);
  }
});

test("MCP source verification receives the literal release tag via environment", () => {
  const step = workflow.jobs.publish.steps.find(
    (item) => item.name === "Verify tagged source and required checks",
  );
  assert.equal(step.env.RELEASE_TAG, "${{ inputs.release_tag }}");
  assert.ok(step.run.includes('--tag "${RELEASE_TAG}"'));
});

test("workflow inline scripts never interpolate dispatch or event data", () => {
  for (const name of fs.readdirSync(path.join(root, ".github/workflows"))) {
    if (!/\.ya?ml$/.test(name)) continue;
    const source = fs.readFileSync(
      path.join(root, ".github/workflows", name),
      "utf8",
    );
    const value = parseYAMLMapping(source, name);
    for (const job of Object.values(value.jobs)) {
      for (const step of job.steps || []) {
        if (typeof step.run !== "string") continue;
        assert.doesNotMatch(
          step.run,
          /\$\{\{\s*(?:inputs\b|github\.event\b|github\.head_ref\b)/,
          name,
        );
      }
    }
  }
});
