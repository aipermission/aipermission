import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { prepareLocalActionRetry, preserveLocalActionRetryAttempt, releaseLocalActionRetryAttempt } from "../local-action-retry.ts";
import { allEntries } from "./entries.ts";
import { observeLocalActionRetryResponse } from "./observations.ts";
import { memoryEntries, resetRetryStorage } from "./storage.ts";
import { connectorApprovalFixture } from "../../test/connector-action-fixtures.ts";
import type { ConnectorApproval } from "../gateway-contracts/security-contracts.ts";

const workspace = "native-observation-fixture";
const scope = { key: workspace };
const body = { path: "/api/connector-actions/local-run", body: { target_ref: "example:3:11", action_name: "example_action", input: {} } };
const detailPath = "/api/connector-action-approvals/71";

afterEach(async () => resetRetryStorage());

async function pendingRequest() {
  const prepared = await prepareLocalActionRetry(body, { workspaceID: workspace });
  await preserveLocalActionRetryAttempt(prepared, { request_id: 71, target_ref: body.body.target_ref, action_name: body.body.action_name });
  return prepared;
}

function approval(status: ConnectorApproval["status"], extra: Partial<ConnectorApproval> = {}) {
  return connectorApprovalFixture({ id: 71, status, ...extra });
}

for (const kind of ["detail", "list"]) {
  for (const status of ["completed", "failed", "canceled", "blocked", "stale", "declined", "error"] as const) {
    test(`native retry settles an exact ${kind} ${status} observation`, async () => {
      await pendingRequest();
      const value = approval(status);
      await observeLocalActionRetryResponse(
        kind === "detail" ? detailPath : "/api/connector-action-approvals?active=true",
        kind === "detail" ? value : [value],
        workspace,
      );
      assert.deepEqual(await allEntries(scope), []);
    });
  }
}

for (const status of ["running", "approval_pending", "outcome_unknown"] as const) {
  test(`native retry retains ${status} without authorizing a fresh mutation`, async () => {
    const prepared = await pendingRequest();
    await observeLocalActionRetryResponse(detailPath, approval(status, { assistant_hint: "Reconcile the existing request." }), workspace);
    const [entry] = await allEntries(scope);
    assert.equal(entry.state, status === "outcome_unknown" ? "outcome_unknown" : "pending");
    if (status === "outcome_unknown") {
      assert.equal(entry.assistant_hint, "Reconcile the existing request.");
      await assert.rejects(prepareLocalActionRetry(body, { workspaceID: workspace }), { code: "local_action_reconciliation_canceled" });
    } else {
      const retry = await prepareLocalActionRetry(body, { workspaceID: workspace });
      assert.equal(retry.idempotencyKey, prepared.idempotencyKey);
      assert.equal(retry.reused, true);
      await releaseLocalActionRetryAttempt(retry);
    }
  });
}

test("native observation ignores unrelated routes, workspaces, targets and actions", async () => {
  await pendingRequest();
  for (const [path, binding] of [
    ["/api/status", workspace],
    [detailPath, ""],
    [detailPath, "other-workspace"],
  ]) {
    await observeLocalActionRetryResponse(path, approval("completed"), binding);
    assert.equal((await allEntries(scope)).length, 1);
  }
  for (const extra of [{ target_ref: "example:4:11" }, { action_name: "other_action" }]) {
    await observeLocalActionRetryResponse(detailPath, approval("completed", extra), workspace);
    assert.equal((await allEntries(scope)).length, 1);
  }
  await assert.rejects(observeLocalActionRetryResponse(detailPath, approval("completed", { id: 72 }), workspace), /identity/);
  await assert.rejects(observeLocalActionRetryResponse(detailPath, {}, workspace), /Invalid/);
  assert.equal((await allEntries(scope)).length, 1);
});

test("a terminal observation cannot retire a concurrent active attempt", async () => {
  const first = await prepareLocalActionRetry(body, { workspaceID: workspace });
  const active = await prepareLocalActionRetry(body, { workspaceID: workspace });
  await preserveLocalActionRetryAttempt(first, { request_id: 71, target_ref: body.body.target_ref, action_name: body.body.action_name });
  await observeLocalActionRetryResponse(detailPath, approval("completed"), workspace);
  assert.equal((await allEntries(scope)).length, 1);
  await releaseLocalActionRetryAttempt(active);
  await observeLocalActionRetryResponse(detailPath, approval("completed"), workspace);
  assert.deepEqual(await allEntries(scope), []);
});

test("corrupt storage retains the operation and emits only the fixed recovery diagnostic", async (t) => {
  const prepared = await pendingRequest();
  const entries = memoryEntries.get(workspace);
  assert.ok(entries);
  const entry = entries.get(prepared.signature);
  assert.ok(entry);
  entries.set(prepared.signature, { ...entry, revision: -1 });
  const warn = t.mock.method(console, "warn", () => {});
  await observeLocalActionRetryResponse(detailPath, approval("completed"), workspace);
  assert.equal(entries.size, 1);
  assert.equal(warn.mock.calls.length, 1);
  assert.deepEqual(warn.mock.calls[0].arguments, [
    "Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying.",
  ]);
});

for (const [method, status] of [
  ["set", "outcome_unknown"],
  ["delete", "completed"],
] as const) {
  test(`a failed observation ${method} retains the valid retry identity`, async (t) => {
    const prepared = await pendingRequest();
    const entries = memoryEntries.get(workspace);
    assert.ok(entries);
    const original = entries.get(prepared.signature);
    t.mock.method(entries, method, () => {
      throw new Error("private persistence canary");
    });
    const warn = t.mock.method(console, "warn", () => {});
    await observeLocalActionRetryResponse(detailPath, approval(status), workspace);
    assert.deepEqual(entries.get(prepared.signature), original);
    assert.deepEqual(await allEntries(scope), [original]);
    assert.deepEqual(
      warn.mock.calls.map((call) => call.arguments),
      [["Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying."]],
    );
  });
}

test("guarded mutation preparation remains blocked after a pending observation", async () => {
  const prepared = await prepareLocalActionRetry(body, { workspaceID: workspace, exclusiveMutationActions: [body.body.action_name] });
  await preserveLocalActionRetryAttempt(prepared, { request_id: 71 });
  await observeLocalActionRetryResponse(detailPath, approval("running"), workspace);
  await assert.rejects(
    prepareLocalActionRetry(body, { workspaceID: workspace, exclusiveMutationActions: [body.body.action_name] }),
    /protected retry identity changed/,
  );
  assert.equal((await allEntries(scope))[0].key, prepared.idempotencyKey);
});
