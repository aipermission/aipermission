import assert from "node:assert/strict";
import test from "node:test";
import { setImmediate } from "node:timers/promises";

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
  let refreshCalls = 0;
  const { states, options } = runnerOptions({
    post: async () => ({ status: "approval_pending", display_text: "Waiting for approval" }),
    onRefreshActivity: async () => {
      refreshCalls += 1;
    },
  });

  assert.equal(await runGuardedConnectorAction(options), null);
  await settleAsyncRefresh();
  assert.equal(refreshCalls, 1);
  assert.deepEqual(states.at(-1), { state: "idle", error: "", message: "Waiting for approval" });
});

test("guarded connector action surfaces approval activity refresh failures", async () => {
  const { states, options } = runnerOptions({
    post: async () => ({ status: "approval_pending", display_text: "Waiting for approval" }),
    onRefreshActivity: async () => {
      throw new Error("refresh unavailable");
    },
  });

  assert.equal(await runGuardedConnectorAction(options), null);
  await settleAsyncRefresh();
  assert.deepEqual(states.at(-1), {
    state: "idle",
    error: "Approval is pending, but activity refresh failed: refresh unavailable",
    message: "Waiting for approval",
  });
});

test("guarded connector action safely formats non-Error refresh failures", async () => {
  const { states, options } = runnerOptions({
    post: async () => ({ status: "approval_pending" }),
    onRefreshActivity: async () => Promise.reject(null),
  });

  assert.equal(await runGuardedConnectorAction(options), null);
  await settleAsyncRefresh();
  assert.match(states.at(-1).error, /unknown error/);
});

async function settleAsyncRefresh() {
  await setImmediate();
}
