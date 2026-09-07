import { localActionReconciliationEvent, localActionRetryLedgerChangedEvent } from "./local-action-retry/constants.js";
import {
  allEntries,
  completeEntryAttempt,
  deleteEntryIfMatching,
  getEntry,
  replaceReconciledEntry,
  releaseEntryAttempt,
  reserveEntry,
  updateEntryIfMatching,
} from "./local-action-retry/entries.js";
import { retryIdentityChangedError } from "./local-action-retry/errors.js";
import { stableRequestSignature, validRetryEntry } from "./local-action-retry/records.js";
import {
  assertNoLegacyLedger,
  currentRetryScope,
  notifyChanged,
  readLegacyLedger,
  removeLegacyLedger,
  requestReconciliation,
} from "./local-action-retry/runtime.js";
import { releaseSigningReservation, reserveSigningKey } from "./local-action-retry/signing.js";
import { resetRetryStorage } from "./local-action-retry/storage.js";

export { localActionReconciliationEvent, localActionRetryLedgerChangedEvent };

export async function prepareLocalActionRetry(body) {
  const scope = currentRetryScope();
  assertNoLegacyLedger(scope);
  const signedRequest = await requestSignature(scope, body || {});
  let reservationActive = true;
  try {
    let existing = await getEntry(scope, signedRequest.signature);
    let reconciled = false;
    if (existing?.state === "outcome_unknown") {
      const confirmed = await requestReconciliation(existing);
      if (!confirmed) {
        const error = new Error("A new external attempt was canceled. The unresolved request remains protected.");
        error.code = "local_action_reconciliation_canceled";
        throw error;
      }
      existing = await replaceReconciledEntry(scope, existing);
      reconciled = true;
    }
    const reservation = await reserveEntry(scope, signedRequest.signature, signedRequest.reservationID);
    reservationActive = false;
    return {
      scope,
      signature: signedRequest.signature,
      idempotencyKey: reservation.entry.key,
      revision: reservation.entry.revision,
      attemptID: reservation.attempt.id,
      reused: !reservation.created && !reconciled,
    };
  } finally {
    if (reservationActive) await releaseSigningReservation(scope, signedRequest.reservationID);
  }
}

export async function markLocalActionRetryOutcome(prepared, data) {
  if (!prepared?.scope || !prepared.signature) return;
  const changed = await updateEntryIfMatching(prepared, (entry) => ({
    ...entry,
    state: "outcome_unknown",
    revision: entry.revision + 1,
    request_id: Number.isSafeInteger(data?.request_id) ? data.request_id : null,
    assistant_hint: String(data?.assistant_hint || "").slice(0, 1024),
    updated_at: new Date().toISOString(),
  }));
  if (changed) return;
  const current = await getEntry(prepared.scope, prepared.signature);
  if (
    current?.key === prepared.idempotencyKey &&
    current.state === "outcome_unknown" &&
    current.revision > prepared.revision &&
    validRetryEntry(current, prepared.scope.key, prepared.signature)
  ) {
    return;
  }
  throw retryIdentityChangedError();
}

export async function completeLocalActionRetry(prepared) {
  if (!prepared?.scope || !prepared.signature) return;
  return completeEntryAttempt(prepared);
}

export async function releaseLocalActionRetryAttempt(prepared) {
  if (!prepared?.scope || !prepared.signature) return;
  return releaseEntryAttempt(prepared);
}

export async function preserveLocalActionRetryAttempt(prepared) {
  if (!prepared?.scope || !prepared.signature) return;
  const changed = await updateEntryIfMatching(prepared, (entry) => ({
    ...entry,
    revision: entry.revision + 1,
    updated_at: new Date().toISOString(),
  }));
  if (changed) return;
  const current = await getEntry(prepared.scope, prepared.signature);
  if (
    current?.key === prepared.idempotencyKey &&
    current.revision > prepared.revision &&
    validRetryEntry(current, prepared.scope.key, prepared.signature)
  ) {
    return;
  }
  throw retryIdentityChangedError();
}

export async function listLocalActionRetryEntries() {
  const scope = currentRetryScope();
  if (readLegacyLedger(scope)) {
    return [
      {
        signature: "legacy-v2-ledger",
        key: "",
        state: "outcome_unknown",
        created_at: "",
        updated_at: "",
        assistant_hint: "A retry ledger from an earlier version requires manual reconciliation before it can be removed.",
        invalid: true,
      },
    ];
  }
  const entries = await allEntries(scope);
  return entries.sort((left, right) => String(right.updated_at).localeCompare(String(left.updated_at)));
}

export async function resolveLocalActionRetryEntry(entry) {
  const scope = currentRetryScope();
  if (entry?.signature === "legacy-v2-ledger") {
    removeLegacyLedger(scope);
    notifyChanged();
    return true;
  }
  if (!validRetryEntry(entry, scope.key)) throw retryIdentityChangedError();
  return deleteEntryIfMatching(scope, entry.signature, entry.key, entry.revision);
}

export async function resetLocalActionRetryLedger() {
  const scope = currentRetryScope();
  removeLegacyLedger(scope);
  await resetRetryStorage();
  notifyChanged();
}

async function requestSignature(scope, body) {
  const cryptoAPI = globalThis.crypto;
  if (!cryptoAPI?.subtle || typeof TextEncoder === "undefined") {
    throw new Error("Secure request hashing is unavailable; the connector action was not sent.");
  }
  const reservation = await reserveSigningKey(scope);
  try {
    const signature = await cryptoAPI.subtle.sign("HMAC", reservation.key, new TextEncoder().encode(stableRequestSignature(body)));
    return {
      signature: Array.from(new Uint8Array(signature), (byte) => byte.toString(16).padStart(2, "0")).join(""),
      reservationID: reservation.id,
    };
  } catch (error) {
    await releaseSigningReservation(scope, reservation.id);
    throw error;
  }
}
