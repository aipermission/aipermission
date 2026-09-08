import {
  actionAttemptLifetimeMs,
  attemptsStore,
  entriesStore,
  keysStore,
  reservationsStore,
  signingReservationLifetimeMs,
} from "./constants.js";

export function newRetryEntry(scope, signature) {
  const now = new Date().toISOString();
  return {
    id: entryID(scope.key, signature),
    scope: scope.key,
    signature,
    key: newIdempotencyKey(),
    state: "pending",
    revision: 1,
    created_at: now,
    updated_at: now,
  };
}

export function newSigningReservation(scope) {
  const now = Date.now();
  return {
    id: newIdempotencyKey(),
    scope: scope.key,
    created_at: new Date(now).toISOString(),
    expires_at: new Date(now + signingReservationLifetimeMs).toISOString(),
  };
}

export function newActionAttempt(scope, entry, attemptID) {
  const now = Date.now();
  return {
    id: attemptID,
    scope: scope.key,
    entry_id: entry.id,
    signature: entry.signature,
    key: entry.key,
    revision: entry.revision,
    created_at: new Date(now).toISOString(),
    expires_at: new Date(now + actionAttemptLifetimeMs).toISOString(),
  };
}

export function validRetryEntry(entry, scope, signature = "") {
  return (
    entry !== null &&
    typeof entry === "object" &&
    entry.id === entryID(scope, entry.signature) &&
    entry.scope === scope &&
    /^[a-f0-9]{64}$/.test(entry.signature) &&
    (!signature || entry.signature === signature) &&
    typeof entry.key === "string" &&
    entry.key.length > 0 &&
    entry.key.length <= 128 &&
    (entry.state === "pending" || entry.state === "outcome_unknown") &&
    Number.isSafeInteger(entry.revision) &&
    entry.revision > 0 &&
    typeof entry.created_at === "string" &&
    typeof entry.updated_at === "string"
  );
}

export function sameRetryEntry(current, expected) {
  return (
    validRetryEntry(current, expected.scope, expected.signature) &&
    current.key === expected.key &&
    current.revision === expected.revision &&
    current.state === expected.state
  );
}

export function validSigningKeyRecord(record, scope) {
  const key = record?.key;
  return (
    typeof scope === "string" &&
    scope.length > 0 &&
    record?.scope === scope &&
    key !== null &&
    typeof key === "object" &&
    key.type === "secret" &&
    key.extractable === false &&
    key.algorithm?.name === "HMAC" &&
    Array.isArray(key.usages) &&
    key.usages.includes("sign")
  );
}

export function validSigningReservation(record, scope = "", reservationID = "") {
  return (
    record !== null &&
    typeof record === "object" &&
    typeof record.id === "string" &&
    record.id.length > 0 &&
    (!reservationID || record.id === reservationID) &&
    typeof record.scope === "string" &&
    record.scope.length > 0 &&
    (!scope || record.scope === scope) &&
    typeof record.created_at === "string" &&
    Number.isFinite(Date.parse(record.created_at)) &&
    typeof record.expires_at === "string" &&
    Number.isFinite(Date.parse(record.expires_at))
  );
}

export function validActionAttempt(record, expected = {}) {
  return (
    validAttemptIdentity(record, expected) &&
    validAttemptRetryIdentity(record, expected) &&
    validTimestamp(record.created_at) &&
    validTimestamp(record.expires_at)
  );
}

function validAttemptIdentity(record, expected) {
  return (
    record !== null &&
    typeof record === "object" &&
    typeof record.id === "string" &&
    record.id.length > 0 &&
    (!expected.id || record.id === expected.id) &&
    typeof record.scope === "string" &&
    record.scope.length > 0 &&
    (!expected.scope || record.scope === expected.scope) &&
    record.entry_id === entryID(record.scope, record.signature) &&
    (!expected.entryID || record.entry_id === expected.entryID)
  );
}

function validAttemptRetryIdentity(record, expected) {
  return (
    /^[a-f0-9]{64}$/.test(record.signature) &&
    (!expected.signature || record.signature === expected.signature) &&
    typeof record.key === "string" &&
    record.key.length > 0 &&
    record.key.length <= 128 &&
    (!expected.key || record.key === expected.key) &&
    Number.isSafeInteger(record.revision) &&
    record.revision > 0 &&
    (!expected.revision || record.revision === expected.revision)
  );
}

function validTimestamp(value) {
  return typeof value === "string" && Number.isFinite(Date.parse(value));
}

export function validRetryDatabaseSchema(database) {
  if (
    !database.objectStoreNames.contains(entriesStore) ||
    !database.objectStoreNames.contains(keysStore) ||
    !database.objectStoreNames.contains(reservationsStore) ||
    !database.objectStoreNames.contains(attemptsStore)
  ) {
    return false;
  }
  const transaction = database.transaction([entriesStore, reservationsStore, attemptsStore], "readonly");
  return (
    transaction.objectStore(entriesStore).indexNames.contains("scope") &&
    transaction.objectStore(reservationsStore).indexNames.contains("scope") &&
    transaction.objectStore(reservationsStore).indexNames.contains("expires_at") &&
    transaction.objectStore(attemptsStore).indexNames.contains("scope") &&
    transaction.objectStore(attemptsStore).indexNames.contains("entry_id") &&
    transaction.objectStore(attemptsStore).indexNames.contains("expires_at")
  );
}

export function entryID(scope, signature) {
  return `${scope}:${signature}`;
}

export function stableRequestSignature(value) {
  if (Array.isArray(value)) return `[${value.map(stableRequestSignature).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableRequestSignature(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}

function newIdempotencyKey() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID();
  if (!globalThis.crypto?.getRandomValues) {
    throw new Error("Secure request identity generation is unavailable; the connector action was not sent.");
  }
  const bytes = globalThis.crypto.getRandomValues(new Uint8Array(16));
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
