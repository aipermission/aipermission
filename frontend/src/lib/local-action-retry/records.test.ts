import assert from "node:assert/strict";
import { test } from "node:test";
import { newRetryEntry, newSigningReservation, newActionAttempt, validRetryEntry, validSigningReservation, validActionAttempt, validSigningKeyRecord, sameRetryEntry, stableRequestSignature } from "./records.ts";

const scope = { key: "workspace-a" };
const signature = "a".repeat(64);

test("retry record constructors preserve scope, revision and attempt identity", () => {
  const entry = newRetryEntry(scope, signature);
  assert.equal(entry.id, `workspace-a:${signature}`);
  assert.equal(entry.revision, 1);
  assert.equal(entry.state, "pending");
  assert.equal(validRetryEntry(entry, scope.key, signature), true);
  const reservation = newSigningReservation(scope);
  assert.equal(validSigningReservation(reservation, scope.key, reservation.id), true);
  const attempt = newActionAttempt(scope, entry, reservation.id);
  assert.equal(validActionAttempt(attempt, { id: reservation.id, scope: scope.key, signature, entryID: entry.id, key: entry.key, revision: 1 }), true);
  assert.equal(validActionAttempt(attempt, { revision: 2 }), false);
  assert.equal(validSigningReservation(reservation, "other-workspace"), false);
});

test("retry predicates reject malformed persisted data without property coercion", () => {
  const entry = newRetryEntry(scope, signature);
  for (const value of [null, [], {}, { ...entry, signature: {} }, { ...entry, revision: "1" }, { ...entry, key: "" }, { ...entry, operation_ref: 3 }, { ...entry, created_at: null }, { ...entry, state: "completed" }]) {
    assert.equal(validRetryEntry(value, scope.key), false);
  }
  for (const value of [null, [], {}, { id: "one", scope: scope.key, created_at: "invalid", expires_at: "invalid" }]) {
    assert.equal(validSigningReservation(value), false);
    assert.equal(validActionAttempt(value), false);
  }
  assert.equal(sameRetryEntry({ ...entry }, entry), true);
  assert.equal(sameRetryEntry({ ...entry, revision: 2 }, entry), false);
  assert.equal(sameRetryEntry({ ...entry, state: "retired" }, entry), false);
});

test("signing key validation accepts non-extractable HMAC keys and rejects malformed usage", async () => {
  const key = await globalThis.crypto.subtle.generateKey({ name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  assert.equal(validSigningKeyRecord({ scope: scope.key, key }, scope.key), true);
  assert.equal(validSigningKeyRecord({ scope: scope.key, key }, "other-workspace"), false);
  for (const value of [null, [], {}, { scope: scope.key, key: null }, { scope: scope.key, key: { type: "secret", extractable: false, algorithm: { name: "HMAC" }, usages: ["sign", 3] } }]) {
    assert.equal(validSigningKeyRecord(value, scope.key), false);
  }
});

test("stable signatures sort object fields while preserving arrays and undefined semantics", () => {
  assert.equal(stableRequestSignature({ b: 2, a: [true, null, "text"] }), '{"a":[true,null,"text"],"b":2}');
  assert.equal(stableRequestSignature({ a: undefined }), '{"a":undefined}');
  assert.equal(stableRequestSignature(undefined), undefined);
});
