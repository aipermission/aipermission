import { localActionReconciliationEvent, localActionRetryLedgerChangedEvent } from "./local-action-retry/constants.ts";
import {
  allEntries,
  completeEntryAttempt,
  deleteEntryIfMatching,
  getEntry,
  replaceReconciledEntry,
  releaseEntryAttempt,
  reserveEntry,
  retireEntryAttempt,
  updateEntryIfMatching,
} from "./local-action-retry/entries.ts";
import { retryIdentityChangedError } from "./local-action-retry/errors.ts";
import { stableRequestSignature, validRetryEntry } from "./local-action-retry/records.ts";
import type { PreparedRetry, RetryEntry, RetryScope } from "./local-action-retry/records.ts";
import {
  assertNoLegacyLedger,
  currentRetryScope,
  notifyChanged,
  readLegacyLedger,
  removeLegacyLedger,
  requestReconciliation,
} from "./local-action-retry/runtime.ts";
import { releaseSigningReservation, reserveSigningKey } from "./local-action-retry/signing.ts";
import { resetRetryStorage } from "./local-action-retry/storage.ts";

export { localActionReconciliationEvent, localActionRetryLedgerChangedEvent };

export type LegacyRetryEntry = {
  signature: "legacy-v2-ledger";
  key: string;
  state: "outcome_unknown";
  created_at: string;
  updated_at: string;
  assistant_hint: string;
  invalid: true;
};
export type RetryListEntry = RetryEntry | LegacyRetryEntry;

export async function prepareLocalActionRetry(body: unknown, options: { workspaceID?: string } = {}): Promise<PreparedRetry> {
  const scope = currentRetryScope(options.workspaceID);
  assertNoLegacyLedger(scope);
  const signedRequest = await requestSignature(scope, body || {});
  let reservationActive = true;
  try {
    let existing = await getEntry(scope, signedRequest.signature);
    let reconciled = false;
    if (existing?.state === "outcome_unknown") {
      const confirmed = await requestReconciliation(existing);
      if (!confirmed) {
        throw Object.assign(new Error("A new external attempt was canceled. The unresolved request remains protected."), {
          code: "local_action_reconciliation_canceled",
        });
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

export async function markLocalActionRetryOutcome(prepared: unknown, value: unknown) {
  if (!validPreparedRetry(prepared)) return;
  const data = objectRecord(value);
  const changed = await updateEntryIfMatching(
    prepared,
    (entry) => ({
      ...entry,
      state: "outcome_unknown",
      revision: entry.revision + 1,
      ...acknowledgedRequestIdentity(entry, data, true),
      operation_ref: localActionOperationRef(data) || entry.operation_ref,
      assistant_hint: String(data?.assistant_hint || entry.assistant_hint || "").slice(0, 1024),
      updated_at: new Date().toISOString(),
    }),
    true,
  );
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

function localActionOperationRef(data: Record<string, unknown> | null) {
  if (typeof data?.operation_ref === "string") return data.operation_ref.trim().slice(0, 128);
  if (typeof data?.operation_id === "number" && Number.isSafeInteger(data.operation_id) && data.operation_id > 0)
    return `operation:${data.operation_id}`;
  return "";
}

export async function completeLocalActionRetry(prepared: unknown, acknowledgedTerminal = false, requestID?: number) {
  if (!validPreparedRetry(prepared)) return;
  return completeEntryAttempt(prepared, acknowledgedTerminal, requestID);
}

export async function releaseLocalActionRetryAttempt(prepared: unknown) {
  if (!validPreparedRetry(prepared)) return;
  await releaseEntryAttempt(prepared);
}

export async function retireLocalActionRetryAttempt(prepared: unknown) {
  if (!validPreparedRetry(prepared)) return;
  return retireEntryAttempt(prepared);
}

export async function preserveLocalActionRetryAttempt(prepared: unknown, value?: unknown) {
  if (!validPreparedRetry(prepared)) return;
  const data = objectRecord(value);
  const changed = await updateEntryIfMatching(
    prepared,
    (entry) => ({
      ...entry,
      ...acknowledgedRequestIdentity(entry, data),
      revision: entry.revision + 1,
      updated_at: new Date().toISOString(),
    }),
    true,
  );
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

function acknowledgedRequestIdentity(entry: RetryEntry, data: Record<string, unknown> | null, retainConflicting = false) {
  if (typeof data?.request_id !== "number" || !Number.isSafeInteger(data.request_id) || data.request_id < 1) return {};
  if (entry.request_id != null && entry.request_id !== data.request_id) {
    if (retainConflicting) return {};
    throw retryIdentityChangedError();
  }
  return { request_id: data.request_id };
}

export async function listLocalActionRetryEntries(): Promise<RetryListEntry[]> {
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

export async function resolveLocalActionRetryEntry(entry: unknown) {
  const scope = currentRetryScope();
  if (objectRecord(entry)?.signature === "legacy-v2-ledger") {
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

async function requestSignature(scope: RetryScope, body: unknown) {
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

function objectRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

function validPreparedRetry(value: unknown): value is PreparedRetry {
  const prepared = objectRecord(value);
  const scope = objectRecord(prepared?.scope);
  return (
    !!prepared &&
    !!scope &&
    typeof scope.key === "string" &&
    scope.key.length > 0 &&
    (scope.legacyKey === undefined || typeof scope.legacyKey === "string") &&
    typeof prepared.signature === "string" &&
    /^[a-f0-9]{64}$/.test(prepared.signature) &&
    typeof prepared.idempotencyKey === "string" &&
    prepared.idempotencyKey.length > 0 &&
    typeof prepared.revision === "number" &&
    Number.isSafeInteger(prepared.revision) &&
    prepared.revision > 0 &&
    typeof prepared.attemptID === "string" &&
    prepared.attemptID.length > 0 &&
    typeof prepared.reused === "boolean"
  );
}
