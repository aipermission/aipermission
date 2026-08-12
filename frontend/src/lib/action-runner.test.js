import assert from "node:assert/strict";
import test from "node:test";

import { runGuardedConnectorAction } from "../connectors/templates/_shared/action-runner.js";
import { createRequestGuard } from "../connectors/templates/_shared/request-guard.js";

function runnerOptions(overrides = {}) {
  const states = [];
  return {
    states,
    options: {
      requestGuard: createRequestGuard("target:1"),
      channel: "list",
      targetRef: "test:1:1",
      actionName: "list_items",
      reason: "test action",
      product: "Test",
      setState: (state) => states.push(state),
      ...overrides,
    },
  };
}

test("guarded connector action ignores a response from an old target scope", async () => {
  let resolveResponse;
  const post = () => new Promise((resolve) => (resolveResponse = resolve));
  const { states, options } = runnerOptions({ post });
  const promise = runGuardedConnectorAction(options);
  options.requestGuard.setScope("target:2");
  resolveResponse({ status: "completed", output: { ok: true } });

  assert.equal(await promise, null);
  assert.deepEqual(states, [{ state: "running", error: "", message: "" }]);
});

test("guarded connector action rejects a failed HTTP 200 action result", async () => {
  const { states, options } = runnerOptions({
    post: async () => ({ status: "failed", error: "remote failure" }),
  });

  await assert.rejects(() => runGuardedConnectorAction(options), /remote failure/);
  assert.deepEqual(states.at(-1), { state: "error", error: "remote failure", message: "" });
});

test("guarded connector action keeps approval-pending separate from completion", async () => {
  const { states, options } = runnerOptions({
    post: async () => ({ status: "approval_pending", display_text: "Waiting for approval" }),
  });

  assert.equal(await runGuardedConnectorAction(options), null);
  assert.deepEqual(states.at(-1), { state: "idle", error: "", message: "Waiting for approval" });
});
