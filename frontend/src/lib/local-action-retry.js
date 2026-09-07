import { scopedUICookieName } from "./ui-cookie.js";

export const localActionReconciliationEvent = "aipermission:local-action-reconciliation-required";
export const localActionRetryLedgerChangedEvent = "aipermission:local-action-retry-ledger-changed";

const legacyStoragePrefix = "aipermission.local-action-retry.v2.";
const databaseName = "aipermission-local-action-retry";
const databaseVersion = 2;
const entriesStore = "entries";
const keysStore = "keys";
const reservationsStore = "reservations";
const workspaceCookieName = "aipermission_workspace";
const maxEntries = 128;
const maxGlobalEntries = 512;
const maxRetryScopes = 64;
const signingReservationLifetimeMs = 2 * 60 * 1000;
const memoryEntries = new Map();
const memoryKeys = new Map();
const memoryReservations = new Map();
let memoryQueue = Promise.resolve();
let retryDatabasePromise;
let retryDatabase;

export async function prepareLocalActionRetry(body) {
  const scope = currentRetryScope();
  assertNoLegacyLedger(scope);
  const signedRequest = await requestSignature(scope, body || {});
  let reservationActive = true;
  try {
    let existing = await getEntry(scope, signedRequest.signature);
    if (existing?.state === "outcome_unknown") {
      const confirmed = await requestReconciliation(existing);
      if (!confirmed) {
        const error = new Error("A new external attempt was canceled. The unresolved request remains protected.");
        error.code = "local_action_reconciliation_canceled";
        throw error;
      }
      existing = await replaceReconciledEntry(scope, existing);
      return {
        scope,
        signature: signedRequest.signature,
        idempotencyKey: existing.key,
        revision: existing.revision,
        reused: false,
      };
    }
    const reservation = await reserveEntry(scope, signedRequest.signature, signedRequest.reservationID);
    reservationActive = false;
    return {
      scope,
      signature: signedRequest.signature,
      idempotencyKey: reservation.entry.key,
      revision: reservation.entry.revision,
      reused: !reservation.created,
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
  if (!changed) throw retryIdentityChangedError();
}

export async function completeLocalActionRetry(prepared) {
  if (!prepared?.scope || !prepared.signature) return;
  await deleteEntryIfMatching(prepared.scope, prepared.signature, prepared.idempotencyKey, prepared.revision);
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
  if (isBrowserRuntime()) {
    requireBrowserIndexedDB();
    if (retryDatabase) retryDatabase.close();
    retryDatabase = undefined;
    retryDatabasePromise = undefined;
    await deleteRetryDatabase();
  } else {
    await withMemoryTransaction(() => {
      memoryEntries.clear();
      memoryKeys.clear();
      memoryReservations.clear();
    });
  }
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

async function reserveSigningKey(scope) {
  const cryptoAPI = globalThis.crypto;
  if (!cryptoAPI?.subtle) throw new Error("Secure request hashing is unavailable; the connector action was not sent.");
  const reservation = newSigningReservation(scope);
  if (!usesIndexedDB()) {
    return withMemoryTransaction(async () => {
      if (!memoryKeys.has(scope.key)) {
        memoryKeys.set(scope.key, await cryptoAPI.subtle.generateKey({ name: "HMAC", hash: "SHA-256" }, false, ["sign"]));
      }
      const reservations = memoryReservations.get(scope.key) || new Map();
      reservations.set(reservation.id, reservation);
      memoryReservations.set(scope.key, reservations);
      return { id: reservation.id, key: memoryKeys.get(scope.key) };
    });
  }
  const database = await openRetryDatabase();
  const generated = await cryptoAPI.subtle.generateKey({ name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  return storesTransactionPromise(database, [keysStore, entriesStore, reservationsStore], "readwrite", async (stores) => {
    await removeExpiredSigningReservations(stores, Date.now());
    let current = await requestPromise(stores.keys.get(scope.key));
    if (current !== undefined) {
      if (!validSigningKeyRecord(current, scope.key)) throw storageError();
    } else {
      const scopedEntries = await requestPromise(stores.entries.index("scope").count(scope.key));
      if (scopedEntries > 0) throw storageError();
      await reclaimUnusedSigningKeys(stores);
      const keyCount = await requestPromise(stores.keys.count());
      if (keyCount >= maxRetryScopes) throw ledgerFullError();
      current = { scope: scope.key, key: generated, updated_at: new Date().toISOString() };
      await requestPromise(stores.keys.add(current));
    }
    await requestPromise(stores.reservations.add(reservation));
    return { id: reservation.id, key: current.key };
  });
}

async function reserveEntry(scope, signature, reservationID) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      requireMemorySigningReservation(scope, reservationID);
      const entries = memoryEntries.get(scope.key) || new Map();
      let entry = entries.get(signature);
      if (entry) {
        if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
        removeMemorySigningReservation(scope, reservationID);
        return { entry: { ...entry }, created: false };
      }
      if (!entry) {
        if (entries.size >= maxEntries) throw ledgerFullError();
        const totalEntries = Array.from(memoryEntries.values()).reduce((total, items) => total + items.size, 0);
        if (totalEntries >= maxGlobalEntries) throw ledgerFullError();
        entry = newRetryEntry(scope, signature);
        entries.set(signature, entry);
        memoryEntries.set(scope.key, entries);
        notifyChanged();
      }
      removeMemorySigningReservation(scope, reservationID);
      return { entry: { ...entry }, created: true };
    });
  }
  const database = await openRetryDatabase();
  const reservation = await storesTransactionPromise(database, [entriesStore, reservationsStore], "readwrite", async (stores) => {
    await requireSigningReservation(stores.reservations, scope, reservationID);
    const id = entryID(scope.key, signature);
    let entry = await requestPromise(stores.entries.get(id));
    if (entry) {
      if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
      await requestPromise(stores.reservations.delete(reservationID));
      return { entry, changed: false };
    }
    const count = await requestPromise(stores.entries.index("scope").count(scope.key));
    if (count >= maxEntries) throw ledgerFullError();
    const globalCount = await requestPromise(stores.entries.count());
    if (globalCount >= maxGlobalEntries) throw ledgerFullError();
    entry = newRetryEntry(scope, signature);
    await requestPromise(stores.entries.add(entry));
    await requestPromise(stores.reservations.delete(reservationID));
    return { entry, changed: true };
  });
  if (reservation.changed) notifyChanged();
  return { entry: reservation.entry, created: reservation.changed };
}

async function getEntry(scope, signature) {
  if (!usesIndexedDB()) {
    const entry = memoryEntries.get(scope.key)?.get(signature);
    if (entry && !validRetryEntry(entry, scope.key, signature)) throw storageError();
    return entry;
  }
  const database = await openRetryDatabase();
  const entry = await requestPromise(database.transaction(entriesStore).objectStore(entriesStore).get(entryID(scope.key, signature)));
  if (entry && !validRetryEntry(entry, scope.key, signature)) throw storageError();
  return entry;
}

async function updateEntryIfMatching(prepared, update) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(prepared.scope.key);
      const entry = entries?.get(prepared.signature);
      if (!entry || entry.key !== prepared.idempotencyKey || entry.revision !== prepared.revision) return false;
      if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
      entries.set(prepared.signature, update(entry));
      notifyChanged();
      return true;
    });
  }
  const database = await openRetryDatabase();
  const changed = await transactionPromise(database, entriesStore, "readwrite", async (store) => {
    const id = entryID(prepared.scope.key, prepared.signature);
    const entry = await requestPromise(store.get(id));
    if (!entry || entry.key !== prepared.idempotencyKey || entry.revision !== prepared.revision) return false;
    if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
    await requestPromise(store.put(update(entry)));
    return true;
  });
  if (changed) notifyChanged();
  return changed;
}

async function deleteEntryIfMatching(scope, signature, expectedKey, expectedRevision) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(scope.key);
      const entry = entries?.get(signature);
      if (!entry || entry.key !== expectedKey || entry.revision !== expectedRevision) return false;
      if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
      entries.delete(signature);
      if (entries.size === 0) memoryEntries.delete(scope.key);
      removeUnusedMemorySigningKey(scope);
      notifyChanged();
      return true;
    });
  }
  const database = await openRetryDatabase();
  const changed = await storesTransactionPromise(database, [entriesStore, keysStore, reservationsStore], "readwrite", async (stores) => {
    const id = entryID(scope.key, signature);
    const entry = await requestPromise(stores.entries.get(id));
    if (!entry || entry.key !== expectedKey || entry.revision !== expectedRevision) return false;
    if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
    await requestPromise(stores.entries.delete(id));
    await removeUnusedSigningKey(stores, scope.key);
    return true;
  });
  if (changed) notifyChanged();
  return changed;
}

async function allEntries(scope) {
  if (!usesIndexedDB()) {
    const entries = Array.from(memoryEntries.get(scope.key)?.values() || [], (entry) => ({ ...entry }));
    if (entries.some((entry) => !validRetryEntry(entry, scope.key))) throw storageError();
    return entries;
  }
  const database = await openRetryDatabase();
  const entries = await requestPromise(database.transaction(entriesStore).objectStore(entriesStore).index("scope").getAll(scope.key));
  if (entries.some((entry) => !validRetryEntry(entry, scope.key))) throw storageError();
  return entries;
}

function openRetryDatabase() {
  if (retryDatabasePromise) return retryDatabasePromise;
  retryDatabasePromise = new Promise((resolve, reject) => {
    let settled = false;
    const request = globalThis.indexedDB.open(databaseName, databaseVersion);
    request.onupgradeneeded = () => {
      const database = request.result;
      if (!database.objectStoreNames.contains(entriesStore)) {
        const store = database.createObjectStore(entriesStore, { keyPath: "id" });
        store.createIndex("scope", "scope", { unique: false });
      }
      if (!database.objectStoreNames.contains(keysStore)) database.createObjectStore(keysStore, { keyPath: "scope" });
      if (!database.objectStoreNames.contains(reservationsStore)) {
        const store = database.createObjectStore(reservationsStore, { keyPath: "id" });
        store.createIndex("scope", "scope", { unique: false });
        store.createIndex("expires_at", "expires_at", { unique: false });
      }
    };
    request.onerror = () => {
      if (settled) return;
      settled = true;
      retryDatabasePromise = undefined;
      reject(request.error || storageError());
    };
    request.onsuccess = () => {
      const database = request.result;
      if (settled) {
        database.close();
        return;
      }
      settled = true;
      database.onversionchange = () => {
        database.close();
        retryDatabase = undefined;
        retryDatabasePromise = undefined;
      };
      if (!validRetryDatabaseSchema(database)) {
        database.close();
        settled = true;
        retryDatabasePromise = undefined;
        reject(storageError());
        return;
      }
      retryDatabase = database;
      resolve(database);
    };
    request.onblocked = () => {
      if (settled) return;
      settled = true;
      retryDatabasePromise = undefined;
      reject(storageError());
    };
  });
  return retryDatabasePromise;
}

function transactionPromise(database, storeName, mode, operation) {
  return storesTransactionPromise(database, [storeName], mode, (stores) => operation(stores[storeName]));
}

function storesTransactionPromise(database, storeNames, mode, operation) {
  return new Promise((resolve, reject) => {
    const transaction = database.transaction(storeNames, mode);
    const stores = Object.fromEntries(storeNames.map((storeName) => [storeName, transaction.objectStore(storeName)]));
    let result;
    let operationError;
    try {
      Promise.resolve(operation(stores))
        .then((value) => {
          result = value;
        })
        .catch((error) => {
          operationError = error;
          transaction.abort();
        });
    } catch (error) {
      operationError = error;
      transaction.abort();
    }
    transaction.oncomplete = () => resolve(result);
    transaction.onerror = () => reject(operationError || transaction.error || storageError());
    transaction.onabort = () => reject(operationError || transaction.error || storageError());
  });
}

function requestPromise(request) {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || storageError());
  });
}

function withMemoryTransaction(operation) {
  const next = memoryQueue.then(operation, operation);
  memoryQueue = next.catch(() => {});
  return next;
}

function currentRetryScope() {
  const workspaceID = readCookie(scopedUICookieName(workspaceCookieName));
  if (!workspaceID) throw new Error("Database retry identity is unavailable; the connector action was not sent.");
  return { key: workspaceID, legacyKey: `${legacyStoragePrefix}${workspaceID}` };
}

function assertNoLegacyLedger(scope) {
  if (readLegacyLedger(scope)) {
    throw new Error("An earlier retry ledger requires manual reconciliation in Settings before connector actions can run.");
  }
}

function readLegacyLedger(scope) {
  try {
    return globalThis.window?.localStorage?.getItem(scope.legacyKey) || "";
  } catch {
    throw storageError();
  }
}

function removeLegacyLedger(scope) {
  try {
    globalThis.window?.localStorage?.removeItem(scope.legacyKey);
  } catch {
    throw storageError();
  }
}

function isBrowserRuntime() {
  return typeof window !== "undefined";
}

function usesIndexedDB() {
  if (!isBrowserRuntime()) return false;
  requireBrowserIndexedDB();
  return true;
}

function requireBrowserIndexedDB() {
  if (!globalThis.indexedDB) throw storageError();
}

function newRetryEntry(scope, signature) {
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

function newSigningReservation(scope) {
  const now = Date.now();
  return {
    id: newIdempotencyKey(),
    scope: scope.key,
    created_at: new Date(now).toISOString(),
    expires_at: new Date(now + signingReservationLifetimeMs).toISOString(),
  };
}

async function releaseSigningReservation(scope, reservationID) {
  if (!reservationID) return;
  if (!usesIndexedDB()) {
    await withMemoryTransaction(() => {
      removeMemorySigningReservation(scope, reservationID);
      removeUnusedMemorySigningKey(scope);
    });
    return;
  }
  const database = await openRetryDatabase();
  await storesTransactionPromise(database, [entriesStore, keysStore, reservationsStore], "readwrite", async (stores) => {
    const reservation = await requestPromise(stores.reservations.get(reservationID));
    if (reservation !== undefined) {
      if (!validSigningReservation(reservation, scope.key, reservationID)) throw storageError();
      await requestPromise(stores.reservations.delete(reservationID));
    }
    await removeUnusedSigningKey(stores, scope.key);
  });
}

async function requireSigningReservation(store, scope, reservationID) {
  const reservation = await requestPromise(store.get(reservationID));
  if (!validSigningReservation(reservation, scope.key, reservationID) || Date.parse(reservation.expires_at) <= Date.now()) {
    throw retryIdentityChangedError();
  }
}

function requireMemorySigningReservation(scope, reservationID) {
  const reservation = memoryReservations.get(scope.key)?.get(reservationID);
  if (!validSigningReservation(reservation, scope.key, reservationID) || Date.parse(reservation.expires_at) <= Date.now()) {
    removeMemorySigningReservation(scope, reservationID);
    removeUnusedMemorySigningKey(scope);
    throw retryIdentityChangedError();
  }
}

function removeMemorySigningReservation(scope, reservationID) {
  const reservations = memoryReservations.get(scope.key);
  reservations?.delete(reservationID);
  if (reservations?.size === 0) memoryReservations.delete(scope.key);
}

function removeUnusedMemorySigningKey(scope) {
  if ((memoryEntries.get(scope.key)?.size || 0) > 0) return;
  if ((memoryReservations.get(scope.key)?.size || 0) > 0) return;
  memoryKeys.delete(scope.key);
}

async function removeExpiredSigningReservations(stores, now) {
  const reservations = await requestPromise(stores.reservations.getAll());
  const affectedScopes = new Set();
  for (const reservation of reservations) {
    if (!validSigningReservation(reservation)) throw storageError();
    if (Date.parse(reservation.expires_at) > now) continue;
    affectedScopes.add(reservation.scope);
    await requestPromise(stores.reservations.delete(reservation.id));
  }
  for (const scope of affectedScopes) await removeUnusedSigningKey(stores, scope);
}

async function reclaimUnusedSigningKeys(stores) {
  const records = await requestPromise(stores.keys.getAll());
  for (const record of records) {
    if (!validSigningKeyRecord(record, record?.scope)) throw storageError();
    await removeUnusedSigningKey(stores, record.scope);
  }
}

async function removeUnusedSigningKey(stores, scope) {
  const entryCount = await requestPromise(stores.entries.index("scope").count(scope));
  if (entryCount > 0) return false;
  const reservationCount = await requestPromise(stores.reservations.index("scope").count(scope));
  if (reservationCount > 0) return false;
  await requestPromise(stores.keys.delete(scope));
  return true;
}

async function replaceReconciledEntry(scope, expected) {
  if (!validRetryEntry(expected, scope.key)) throw retryIdentityChangedError();
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(scope.key);
      const current = entries?.get(expected.signature);
      if (!sameRetryEntry(current, expected)) throw retryIdentityChangedError();
      const replacement = newRetryEntry(scope, expected.signature);
      entries.set(expected.signature, replacement);
      notifyChanged();
      return { ...replacement };
    });
  }
  const database = await openRetryDatabase();
  const replacement = await transactionPromise(database, entriesStore, "readwrite", async (store) => {
    const current = await requestPromise(store.get(expected.id));
    if (!sameRetryEntry(current, expected)) throw retryIdentityChangedError();
    const next = newRetryEntry(scope, expected.signature);
    await requestPromise(store.put(next));
    return next;
  });
  notifyChanged();
  return replacement;
}

function validRetryEntry(entry, scope, signature = "") {
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

function sameRetryEntry(current, expected) {
  return (
    validRetryEntry(current, expected.scope, expected.signature) &&
    current.key === expected.key &&
    current.revision === expected.revision &&
    current.state === expected.state
  );
}

function validSigningKeyRecord(record, scope) {
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

function validSigningReservation(record, scope = "", reservationID = "") {
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

function validRetryDatabaseSchema(database) {
  if (
    !database.objectStoreNames.contains(entriesStore) ||
    !database.objectStoreNames.contains(keysStore) ||
    !database.objectStoreNames.contains(reservationsStore)
  ) {
    return false;
  }
  const transaction = database.transaction([entriesStore, reservationsStore], "readonly");
  return (
    transaction.objectStore(entriesStore).indexNames.contains("scope") &&
    transaction.objectStore(reservationsStore).indexNames.contains("scope") &&
    transaction.objectStore(reservationsStore).indexNames.contains("expires_at")
  );
}

function deleteRetryDatabase() {
  return new Promise((resolve, reject) => {
    const request = globalThis.indexedDB.deleteDatabase(databaseName);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error || storageError());
    request.onblocked = () => reject(storageError());
  });
}

function entryID(scope, signature) {
  return `${scope}:${signature}`;
}

function ledgerFullError() {
  return new Error("The local action retry ledger is full. Reconcile unresolved requests in Settings before starting another action.");
}

function storageError() {
  return new Error("Secure retry storage is unavailable; the connector action was not sent.");
}

function retryIdentityChangedError() {
  return new Error("The protected retry identity changed in another tab. Refresh and reconcile the current entry before retrying.");
}

function notifyChanged() {
  if (typeof window !== "undefined" && typeof window.dispatchEvent === "function" && typeof CustomEvent === "function") {
    window.dispatchEvent(new CustomEvent(localActionRetryLedgerChangedEvent));
  }
}

function requestReconciliation(entry) {
  if (typeof window === "undefined") return Promise.resolve(false);
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value) => {
      if (settled) return;
      settled = true;
      resolve(Boolean(value));
    };
    const event = new CustomEvent(localActionReconciliationEvent, {
      cancelable: true,
      detail: {
        requestID: entry.request_id || null,
        assistantHint: entry.assistant_hint || "",
        createdAt: entry.created_at,
        resolve: finish,
      },
    });
    if (window.dispatchEvent(event)) finish(false);
  });
}

function readCookie(name) {
  if (typeof document === "undefined") return "non-browser";
  const prefix = `${name}=`;
  return (
    document.cookie
      .split(";")
      .map((part) => part.trim())
      .find((part) => part.startsWith(prefix))
      ?.slice(prefix.length) || ""
  );
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

function stableRequestSignature(value) {
  if (Array.isArray(value)) return `[${value.map(stableRequestSignature).join(",")}]`;
  if (value && typeof value === "object") {
    return `{${Object.keys(value)
      .sort()
      .map((key) => `${JSON.stringify(key)}:${stableRequestSignature(value[key])}`)
      .join(",")}}`;
  }
  return JSON.stringify(value);
}
