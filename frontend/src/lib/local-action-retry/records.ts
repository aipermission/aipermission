import {
  actionAttemptLifetimeMs,
  attemptsStore,
  entriesStore,
  keysStore,
  reservationsStore,
  signingReservationLifetimeMs,
} from "./constants.ts";

export type RetryScope = { key: string; legacyKey?: string };
export type PreparedRetry = {
  scope: RetryScope;
  signature: string;
  idempotencyKey: string;
  revision: number;
  attemptID: string;
  reused: boolean;
};
export type RetryIdentity = Pick<PreparedRetry, "scope" | "signature" | "idempotencyKey" | "revision">;
export type RetryEntry = {
  id: string;
  scope: string;
  signature: string;
  key: string;
  state: "pending" | "outcome_unknown" | "retired";
  revision: number;
  created_at: string;
  updated_at: string;
  request_id?: number | null;
  target_ref?: string;
  action_name?: string;
  mutation_guard?: boolean;
  operation_ref?: string;
  [field: string]: unknown;
};
export type SigningReservation = { id: string; scope: string; created_at: string; expires_at: string };
export type ActionAttempt = SigningReservation & { entry_id: string; signature: string; key: string; revision: number };
export type AttemptExpectation = { id?: string; scope?: string; entryID?: string; signature?: string; key?: string; revision?: number };
export type SigningKeyRecord = { scope: string; key: CryptoKey; [field: string]: unknown };

function objectRecord(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : null;
}

export function newRetryEntry(scope: RetryScope, signature: string): RetryEntry {
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

export function newSigningReservation(scope: RetryScope): SigningReservation {
  const now = Date.now();
  return {
    id: newIdempotencyKey(),
    scope: scope.key,
    created_at: new Date(now).toISOString(),
    expires_at: new Date(now + signingReservationLifetimeMs).toISOString(),
  };
}

export function newActionAttempt(scope: RetryScope, entry: RetryEntry, attemptID: string): ActionAttempt {
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

export function validRetryEntry(value: unknown, scope: string, signature = ""): value is RetryEntry {
  const entry = objectRecord(value);
  return (
    entry !== null &&
    typeof entry.signature === "string" &&
    entry.id === entryID(scope, entry.signature) &&
    entry.scope === scope &&
    /^[a-f0-9]{64}$/.test(entry.signature) &&
    (!signature || entry.signature === signature) &&
    typeof entry.key === "string" &&
    entry.key.length > 0 &&
    entry.key.length <= 128 &&
    (entry.state === "pending" || entry.state === "outcome_unknown" || entry.state === "retired") &&
    typeof entry.revision === "number" &&
    Number.isSafeInteger(entry.revision) &&
    entry.revision > 0 &&
    validRequestIdentityMetadata(entry) &&
    (entry.mutation_guard === undefined || typeof entry.mutation_guard === "boolean") &&
    (entry.operation_ref === undefined || (typeof entry.operation_ref === "string" && entry.operation_ref.length <= 128)) &&
    typeof entry.created_at === "string" &&
    typeof entry.updated_at === "string"
  );
}

export function retryEntryBlocksMutation(entry: RetryEntry, targetRef: string, actionNames?: readonly string[]) {
  if (entry.state === "retired") return false;
  if (!entry.target_ref || !entry.action_name) return true;
  return entry.target_ref === targetRef && (entry.mutation_guard === true || Boolean(actionNames?.includes(entry.action_name)));
}

function validRequestIdentityMetadata(entry: Record<string, unknown>) {
  return (
    (entry.request_id === undefined ||
      entry.request_id === null ||
      (Number.isSafeInteger(entry.request_id) && (entry.request_id as number) > 0)) &&
    optionalNonemptyString(entry.target_ref) &&
    optionalNonemptyString(entry.action_name)
  );
}

function optionalNonemptyString(value: unknown) {
  return value === undefined || (typeof value === "string" && value.length > 0);
}

export function sameRetryEntry(current: unknown, expected: RetryEntry) {
  return (
    validRetryEntry(current, expected.scope, expected.signature) &&
    current.key === expected.key &&
    current.revision === expected.revision &&
    current.state === expected.state
  );
}

export function validSigningKeyRecord(value: unknown, scope: string): value is SigningKeyRecord {
  const record = objectRecord(value);
  const key = objectRecord(record?.key);
  const algorithm = objectRecord(key?.algorithm);
  return (
    typeof scope === "string" &&
    scope.length > 0 &&
    record?.scope === scope &&
    key !== null &&
    key.type === "secret" &&
    key.extractable === false &&
    algorithm?.name === "HMAC" &&
    Array.isArray(key.usages) &&
    key.usages.every(
      (usage: unknown) =>
        typeof usage === "string" &&
        ["encrypt", "decrypt", "sign", "verify", "deriveKey", "deriveBits", "wrapKey", "unwrapKey"].includes(usage),
    ) &&
    key.usages.includes("sign")
  );
}

export function validSigningReservation(value: unknown, scope = "", reservationID = ""): value is SigningReservation {
  const record = objectRecord(value);
  return (
    record !== null &&
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

export function validActionAttempt(value: unknown, expected: AttemptExpectation = {}): value is ActionAttempt {
  const record = objectRecord(value);
  return (
    record !== null &&
    validAttemptIdentity(record, expected) &&
    validAttemptRetryIdentity(record, expected) &&
    validTimestamp(record.created_at) &&
    validTimestamp(record.expires_at)
  );
}

function validAttemptIdentity(record: Record<string, unknown>, expected: AttemptExpectation) {
  return (
    record !== null &&
    typeof record === "object" &&
    typeof record.id === "string" &&
    record.id.length > 0 &&
    (!expected.id || record.id === expected.id) &&
    typeof record.scope === "string" &&
    record.scope.length > 0 &&
    (!expected.scope || record.scope === expected.scope) &&
    typeof record.signature === "string" &&
    record.entry_id === entryID(record.scope, record.signature) &&
    (!expected.entryID || record.entry_id === expected.entryID)
  );
}

function validAttemptRetryIdentity(record: Record<string, unknown>, expected: AttemptExpectation) {
  return (
    typeof record.signature === "string" &&
    /^[a-f0-9]{64}$/.test(record.signature) &&
    (!expected.signature || record.signature === expected.signature) &&
    typeof record.key === "string" &&
    record.key.length > 0 &&
    record.key.length <= 128 &&
    (!expected.key || record.key === expected.key) &&
    typeof record.revision === "number" &&
    Number.isSafeInteger(record.revision) &&
    record.revision > 0 &&
    (!expected.revision || record.revision === expected.revision)
  );
}

function validTimestamp(value: unknown) {
  return typeof value === "string" && Number.isFinite(Date.parse(value));
}

export function validRetryDatabaseSchema(database: IDBDatabase) {
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

export function entryID(scope: string, signature: string) {
  return `${scope}:${signature}`;
}

export function stableRequestSignature(value: unknown): string | undefined {
  if (Array.isArray(value)) return `[${value.map(stableRequestSignature).join(",")}]`;
  if (value && typeof value === "object") {
    const record = value as Record<string, unknown>;
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableRequestSignature(record[key])}`)
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
