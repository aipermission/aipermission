import assert from "node:assert/strict";
import test from "node:test";
import { isActiveTransferBatch, limitTranscript, mergeConsoleSessionData, normalizeCredentialResources } from "./app-shell-runtime.js";

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
