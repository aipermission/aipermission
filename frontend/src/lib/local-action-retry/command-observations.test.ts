import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { prepareLocalActionRetry, preserveLocalActionRetryAttempt } from "../local-action-retry.ts";
import { allEntries } from "./entries.ts";
import { observeCommandBatchResponse } from "./command-observations.ts";
import { acknowledgedCommandBatch, completedCommandBatch, retainedStatus } from "./command-batches.ts";
import { newRetryEntry } from "./records.ts";
import { memoryEntries, resetRetryStorage } from "./storage.ts";
import type { ConsoleCommandStatus } from "../gateway-contracts/console-command-contract.ts";

const workspace = "native-command-observation";
const scope = { key: workspace };
const path = "/api/console/command-requests/71";
const body = { path: "/api/console/bulk-exec", body: { commands: [] } };
const commands = [
  { request_id: 71, target_id: 11, target_name: "first", status: "running" as const },
  { request_id: 72, target_id: 12, target_name: "second", status: "running" as const },
];
afterEach(async () => resetRetryStorage());

async function pendingBatch() {
  const prepared = await prepareLocalActionRetry(body, { workspaceID: workspace });
  await preserveLocalActionRetryAttempt(prepared, { parallelism: 2, items: commands });
  return prepared;
}

test("a console batch settles only after every exact command has a definitive observation", async () => {
  await pendingBatch();
  assert.equal(await observeCommandBatchResponse("/api/status", {}, workspace), false);
  assert.equal(await observeCommandBatchResponse(path, {}, ""), false);
  await observeCommandBatchResponse(path, { id: 71, runtime_id: 11, status: "completed" }, workspace);
  const [entry] = await allEntries(scope);
  assert.equal(completedCommandBatch(entry), false);
  assert.equal(entry.batch_requests?.[0].observed, true);
  assert.equal(entry.batch_requests?.[1].observed, undefined);
  await observeCommandBatchResponse(
    "/api/console/command-requests/72?detail=true",
    { id: 72, runtime_id: 12, status: "failed" },
    workspace,
  );
  assert.deepEqual(await allEntries(scope), []);
  assert.equal(await observeCommandBatchResponse(path, {}, workspace), true);
});

for (const status of ["untracked", "outcome_unknown"] as const) {
  test(`native command observation retains ${status} and protects later replay`, async () => {
    const prepared = await pendingBatch();
    await observeCommandBatchResponse(path, { id: 71, runtime_id: 11, status }, workspace);
    const [entry] = await allEntries(scope);
    assert.equal(entry.state, "outcome_unknown");
    assert.equal(entry.batch_requests?.[0].status, status);
    assert.equal(completedCommandBatch(entry), false);
    await preserveLocalActionRetryAttempt(prepared, { parallelism: 2, items: commands });
    assert.equal((await allEntries(scope))[0].batch_requests?.[0].status, status);
    await assert.rejects(prepareLocalActionRetry(body, { workspaceID: workspace }), { code: "local_action_reconciliation_canceled" });
  });
}

test("native command reads reject malformed identities and ignore another runtime", async () => {
  await pendingBatch();
  await assert.rejects(observeCommandBatchResponse(path, { id: 72, runtime_id: 11, status: "completed" }, workspace), /Invalid/);
  await observeCommandBatchResponse(path, { id: 71, runtime_id: 99, status: "completed" }, workspace);
  assert.equal((await allEntries(scope))[0].batch_requests?.[0].observed, undefined);
});

test("a late running acknowledgement cannot overwrite an observed terminal command", async () => {
  const prepared = await pendingBatch();
  await observeCommandBatchResponse(path, { id: 71, runtime_id: 11, status: "completed" }, workspace);
  await observeCommandBatchResponse(path, { id: 71, runtime_id: 11, status: "running" }, workspace);
  await preserveLocalActionRetryAttempt(prepared, { parallelism: 2, items: [...commands].reverse() });
  assert.equal((await allEntries(scope))[0].batch_requests?.find((item) => item.request_id === 71)?.status, "completed");
  await assert.rejects(
    preserveLocalActionRetryAttempt(prepared, { parallelism: 2, items: [{ ...commands[0], request_id: 99 }, commands[1]] }),
    /identity changed/,
  );
});

test("batch status merging preserves uncertainty and observed terminal outcomes", () => {
  const plain = newRetryEntry(scope, "a".repeat(64));
  assert.deepEqual(acknowledgedCommandBatch(plain, null), {});
  const batch = { ...plain, request_kind: "console_batch" as const };
  assert.deepEqual(acknowledgedCommandBatch(batch, {}), {});
  for (const [old, incoming, expected] of [
    [undefined, "running", "running"],
    ["running", "completed", "completed"],
    ["completed", "running", "completed"],
    ["running", "untracked", "untracked"],
    ["outcome_unknown", "completed", "outcome_unknown"],
  ] as [ConsoleCommandStatus | undefined, ConsoleCommandStatus, ConsoleCommandStatus][])
    assert.equal(retainedStatus(old, incoming), expected);
  assert.equal(completedCommandBatch(undefined), false);
  assert.equal(completedCommandBatch(batch), false);
});

test("command storage faults emit a fixed diagnostic and retain protected data", async (t) => {
  const prepared = await pendingBatch();
  const entries = memoryEntries.get(workspace);
  assert.ok(entries);
  const entry = entries.get(prepared.signature);
  assert.ok(entry);
  entries.set(prepared.signature, { ...entry, revision: -1 });
  const warn = t.mock.method(console, "warn", () => {});
  assert.equal(await observeCommandBatchResponse(path, { id: 71, runtime_id: 11, status: "completed" }, workspace), true);
  assert.equal(entries.size, 1);
  assert.equal(warn.mock.calls.length, 1);
  assert.deepEqual(warn.mock.calls[0].arguments, [
    "Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying.",
  ]);
});

for (const method of ["set", "delete"] as const) {
  test(`command observation ${method} failures preserve the recorded identity`, async (t) => {
    const prepared = await pendingBatch();
    await observeCommandBatchResponse(path, { id: 71, runtime_id: 11, status: "completed" }, workspace);
    const entries = memoryEntries.get(workspace);
    assert.ok(entries);
    const original = entries.get(prepared.signature);
    t.mock.method(entries, method, () => {
      throw new Error("private command persistence canary");
    });
    const warn = t.mock.method(console, "warn", () => {});
    await observeCommandBatchResponse("/api/console/command-requests/72", { id: 72, runtime_id: 12, status: "completed" }, workspace);
    const [retained] = await allEntries(scope);
    assert.equal(retained?.key, original?.key);
    assert.equal(retained?.state, "pending");
    assert.deepEqual(
      retained?.batch_requests?.map(({ request_id, target_id }) => ({ request_id, target_id })),
      original?.batch_requests?.map(({ request_id, target_id }) => ({ request_id, target_id })),
    );
    if (method === "set") assert.deepEqual(retained, original);
    assert.deepEqual(
      warn.mock.calls.map((call) => call.arguments),
      [["Local retry reconciliation could not be persisted. Protected actions must be reconciled in Settings before retrying."]],
    );
  });
}

test("pending command retries reuse the key while guarded batches reject another submission", async () => {
  const prepared = await pendingBatch();
  const retry = await prepareLocalActionRetry(body, { workspaceID: workspace });
  assert.equal(retry.idempotencyKey, prepared.idempotencyKey);
  assert.equal(retry.reused, true);
  await assert.rejects(
    prepareLocalActionRetry(body, { workspaceID: workspace, exclusiveConsoleBatch: true }),
    /protected retry identity changed/,
  );
});
