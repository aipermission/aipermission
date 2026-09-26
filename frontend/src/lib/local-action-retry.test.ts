import assert from "node:assert/strict";
import test from "node:test";
import {
  completeLocalActionRetry,
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "./local-action-retry.ts";

test("public retry helpers ignore malformed optional prepared identities", async () => {
  for (const value of [null, [], {}, { scope: {} }, { scope: { key: "one" }, signature: "not-a-signature" }]) {
    assert.equal(await completeLocalActionRetry(value), undefined);
    assert.equal(await markLocalActionRetryOutcome(value, {}), undefined);
  }
});

test("uncertain retry results retain typed metadata until explicit reconciliation", async () => {
  await resetLocalActionRetryLedger();
  try {
    const prepared = await prepareLocalActionRetry({ action: "test" });
    await markLocalActionRetryOutcome(prepared, { request_id: 42, operation_id: 13, assistant_hint: "Inspect before retrying" });
    const [entry] = await listLocalActionRetryEntries();
    assert.ok(entry);
    assert.ok("request_id" in entry);
    assert.equal(entry.state, "outcome_unknown");
    assert.equal(entry.request_id, 42);
    assert.equal(entry.operation_ref, "operation:13");
    assert.equal(entry.assistant_hint, "Inspect before retrying");
    await assert.rejects(prepareLocalActionRetry({ action: "test" }), (error: unknown) => {
      assert.ok(error instanceof Error);
      assert.ok("code" in error);
      return error.code === "local_action_reconciliation_canceled";
    });
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
    assert.deepEqual(await listLocalActionRetryEntries(), []);
  } finally {
    await resetLocalActionRetryLedger();
  }
});
