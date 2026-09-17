const assert = require("node:assert/strict");
const test = require("node:test");

const { verifyWorkflowRuntimeContract } = require("../workflow-contracts");

function workflow({
  trigger = "pull_request:",
  group = "${{ github.workflow }}-${{ github.event.pull_request.number || github.run_id }}",
  cancel = "${{ github.event_name == 'pull_request' }}",
  timeout = 15,
} = {}) {
  return `name: Fixture
on:
  ${trigger}
concurrency:
  group: ${group}
  cancel-in-progress: ${cancel}
jobs:
  verify:
    runs-on: ubuntu-latest
    timeout-minutes: ${timeout}
    steps:
      - run: true
`;
}

test("PR workflows use isolated cancellation and bounded jobs", () => {
  assert.doesNotThrow(() => verifyWorkflowRuntimeContract(workflow()));
  assert.throws(
    () => verifyWorkflowRuntimeContract(workflow({ group: "pull-request" })),
    /must equal the canonical/,
  );
  assert.throws(
    () =>
      verifyWorkflowRuntimeContract(
        workflow({ group: "literal-github.workflow-github.event.pull_request.number-github.run_id" }),
      ),
    /must equal the canonical/,
  );
  assert.throws(
    () => verifyWorkflowRuntimeContract(workflow({ cancel: "true" })),
    /cancel only superseded pull-request runs/,
  );
});

test("MCP publications are serialized across release tags", () => {
  assert.doesNotThrow(() =>
    verifyWorkflowRuntimeContract(
      workflow({ trigger: "workflow_dispatch:", group: "publish-mcp", cancel: "false" }),
      ".github/workflows/publish-mcp.yml",
    ),
  );
  assert.throws(
    () =>
      verifyWorkflowRuntimeContract(
        workflow({ trigger: "workflow_dispatch:", group: "publish-mcp-${{ inputs.release_tag }}", cancel: "false" }),
        ".github/workflows/publish-mcp.yml",
      ),
    /serialize every npm publication/,
  );
});

test("non-PR workflows preserve running work", () => {
  assert.doesNotThrow(() =>
    verifyWorkflowRuntimeContract(
      workflow({
        trigger: "workflow_dispatch:",
        group: "publish",
        cancel: "false",
      }),
    ),
  );
  assert.throws(
    () =>
      verifyWorkflowRuntimeContract(
        workflow({ trigger: "schedule:", group: "scheduled", cancel: "true" }),
      ),
    /must not cancel work already in progress/,
  );
});

test("every workflow job has a practical timeout", () => {
  for (const timeout of [0, 61, "fifteen"]) {
    assert.throws(
      () => verifyWorkflowRuntimeContract(workflow({ timeout })),
      /timeout-minutes between 1 and 60/,
    );
  }
});
