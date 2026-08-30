import assert from "node:assert/strict";
import test from "node:test";
import {
  createPollGenerationGuard,
  isActiveTransferBatch,
  limitTranscript,
  mergeConsoleSessionData,
  normalizeCredentialResources,
  parseConsoleSocketMessage,
} from "./app-shell-runtime.js";

test("shell runtime normalizes connector-owned credential resources", () => {
  assert.deepEqual(normalizeCredentialResources("postgres", [{ id: 7, kind: "role" }]), [
    {
      id: 7,
      kind: "role",
      connector_kind: "postgres",
      resource_kind: "role",
      resource_ref: "postgres:role:7",
    },
  ]);
});

test("shell runtime preserves live transcripts and bounds accumulated output", () => {
  const merged = mergeConsoleSessionData(
    [{ id: 3, status: "connected", transcript: "remote", error: null }],
    [{ id: 3, status: "connecting", transcript: "local", error: "waiting" }],
  );
  assert.equal(merged[0].transcript, "local");
  assert.equal(merged[0].status, "connecting");
  assert.equal(limitTranscript("x".repeat(200001)).length, 200000);
  assert.equal(isActiveTransferBatch({ status: "paused" }), true);
  assert.equal(isActiveTransferBatch({ status: "completed" }), false);
});

test("shell polling rejects older and disposed generations", () => {
  const guard = createPollGenerationGuard();
  const first = guard.begin();
  const second = guard.begin();

  assert.equal(guard.isCurrent(first), false);
  assert.equal(guard.isCurrent(second), true);
  guard.invalidate();
  assert.equal(guard.isCurrent(second), false);
});

test("console socket parser accepts typed objects and rejects malformed frames", () => {
  assert.deepEqual(parseConsoleSocketMessage('{"type":"output","data":"ready"}'), { type: "output", data: "ready" });

  for (const value of ["{", "null", "[]", '{"data":"missing type"}', new Uint8Array([1])]) {
    assert.equal(parseConsoleSocketMessage(value), null);
  }
});
