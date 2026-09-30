import { attemptsStore, entriesStore, keysStore, maxActionAttempts, maxEntries, maxGlobalEntries, reservationsStore } from "./constants.ts";
import { ledgerFullError, retryIdentityChangedError, storageError } from "./errors.ts";
import { entryID, newActionAttempt, newRetryEntry, sameRetryEntry, validActionAttempt, validRetryEntry } from "./records.ts";
import type { ActionAttempt, AttemptExpectation, PreparedRetry, RetryEntry, RetryIdentity, RetryScope } from "./records.ts";
import { notifyChanged, usesIndexedDB } from "./runtime.ts";
import {
  requireMemorySigningReservation,
  requireSigningReservation,
  removeMemorySigningReservation,
  removeUnusedMemorySigningKey,
  removeUnusedSigningKey,
} from "./signing.ts";
import {
  memoryEntries,
  memoryAttempts,
  openRetryDatabase,
  requestPromise,
  storesTransactionPromise,
  withMemoryTransaction,
} from "./storage.ts";

type RetryStores = Record<string, IDBObjectStore>;
type RetryEntries = Map<string, RetryEntry>;

export async function reserveEntry(scope: RetryScope, signature: string, reservationID: string) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      requireMemorySigningReservation(scope, reservationID);
      removeExpiredMemoryAttempts();
      const entries = memoryEntries.get(scope.key) || new Map<string, RetryEntry>();
      let entry = entries.get(signature);
      if (entry) {
        if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
        if (entry.state === "outcome_unknown") throw retryIdentityChangedError();
        if (entry.state === "retired") {
          if (hasMemoryAttempts(entry.id)) throw retryIdentityChangedError();
          entries.delete(signature);
          entry = undefined;
        }
      }
      if (entry) {
        const attempt = reserveMemoryAttempt(scope, entry, reservationID);
        removeMemorySigningReservation(scope, reservationID);
        return { entry: { ...entry }, attempt, created: false };
      }
      if (entries.size >= maxEntries) throw ledgerFullError();
      const totalEntries = Array.from(memoryEntries.values()).reduce((total, items) => total + items.size, 0);
      if (totalEntries >= maxGlobalEntries) throw ledgerFullError();
      entry = newRetryEntry(scope, signature);
      entries.set(signature, entry);
      memoryEntries.set(scope.key, entries);
      const attempt = reserveMemoryAttempt(scope, entry, reservationID);
      notifyChanged();
      removeMemorySigningReservation(scope, reservationID);
      return { entry: { ...entry }, attempt, created: true };
    });
  }
  const database = await openRetryDatabase();
  const reservation = await storesTransactionPromise(
    database,
    [entriesStore, reservationsStore, attemptsStore],
    "readwrite",
    async (stores) => {
      await requireSigningReservation(stores.reservations, scope, reservationID);
      await removeExpiredAttempts(stores.attempts, Date.now());
      if ((await requestPromise(stores.attempts.count())) >= maxActionAttempts) throw ledgerFullError();
      const id = entryID(scope.key, signature);
      let entry = await readStoredEntry(stores.entries, scope, signature);
      if (entry) {
        if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
        if (entry.state === "outcome_unknown") throw retryIdentityChangedError();
        if (entry.state === "retired") {
          if ((await requestPromise(stores.attempts.index("entry_id").count(id))) > 0) throw retryIdentityChangedError();
          await requestPromise(stores.entries.delete(id));
          entry = undefined;
        }
      }
      if (entry) {
        const attempt = newActionAttempt(scope, entry, reservationID);
        await requestPromise(stores.attempts.add(attempt));
        await requestPromise(stores.reservations.delete(reservationID));
        return { entry, attempt, changed: false };
      }
      const count = await requestPromise(stores.entries.index("scope").count(scope.key));
      if (count >= maxEntries) throw ledgerFullError();
      const globalCount = await requestPromise(stores.entries.count());
      if (globalCount >= maxGlobalEntries) throw ledgerFullError();
      entry = newRetryEntry(scope, signature);
      await requestPromise(stores.entries.add(entry));
      const attempt = newActionAttempt(scope, entry, reservationID);
      await requestPromise(stores.attempts.add(attempt));
      await requestPromise(stores.reservations.delete(reservationID));
      return { entry, attempt, changed: true };
    },
  );
  if (reservation.changed) notifyChanged();
  return { entry: reservation.entry, attempt: reservation.attempt, created: reservation.changed };
}

export async function getEntry(scope: RetryScope, signature: string) {
  if (!usesIndexedDB()) {
    const entry = memoryEntries.get(scope.key)?.get(signature);
    if (entry && !validRetryEntry(entry, scope.key, signature)) throw storageError();
    return entry;
  }
  const database = await openRetryDatabase();
  return readStoredEntry(database.transaction(entriesStore).objectStore(entriesStore), scope, signature);
}

export async function updateEntryIfMatching(
  prepared: PreparedRetry | RetryIdentity,
  update: (_entry: RetryEntry) => RetryEntry,
  allowNewerRevision = false,
) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(prepared.scope.key);
      const entry = entries?.get(prepared.signature);
      if ("attemptID" in prepared) releaseMemoryAttempt(prepared);
      removeExpiredMemoryAttempts();
      if (retiredIdentity(entry, prepared)) {
        cleanupRetiredMemoryEntry(prepared.scope, entries, entry);
        return true;
      }
      if (!entries || !matchingEntry(entry, prepared, allowNewerRevision)) return false;
      entries.set(prepared.signature, update(entry));
      notifyChanged();
      return true;
    });
  }
  const database = await openRetryDatabase();
  const changed = await storesTransactionPromise(
    database,
    [entriesStore, attemptsStore, keysStore, reservationsStore],
    "readwrite",
    async (stores) => {
      const id = entryID(prepared.scope.key, prepared.signature);
      if ("attemptID" in prepared) await releaseStoredAttempt(stores.attempts, prepared);
      const entry = await readStoredEntry(stores.entries, prepared.scope, prepared.signature);
      if (retiredIdentity(entry, prepared)) {
        await cleanupRetiredStoredEntry(stores, prepared.scope, id, entry);
        return true;
      }
      if (!matchingEntry(entry, prepared, allowNewerRevision)) return false;
      await requestPromise(stores.entries.put(update(entry)));
      return true;
    },
  );
  if (changed) notifyChanged();
  return changed;
}

export async function releaseEntryAttempt(prepared: PreparedRetry) {
  if (!prepared?.attemptID) return;
  return mutateAfterReleasingAttempt(
    prepared,
    (entries, entry) => cleanupRetiredMemoryEntry(prepared.scope, entries, entry),
    (stores, id, entry) => cleanupRetiredStoredEntry(stores, prepared.scope, id, entry),
  );
}

export async function retireEntryAttempt(prepared: PreparedRetry) {
  if (!prepared?.attemptID) return false;
  return mutateAfterReleasingAttempt(
    prepared,
    (entries, entry) => {
      if (!entries || !entry || entry.key !== prepared.idempotencyKey) return false;
      if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
      deleteMemoryAttemptsForEntry(entry);
      entries.delete(prepared.signature);
      if (entries.size === 0) memoryEntries.delete(prepared.scope.key);
      removeUnusedMemorySigningKey(prepared.scope);
      return true;
    },
    async (stores, id, entry) => {
      if (!entry || entry.key !== prepared.idempotencyKey) return false;
      if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
      await deleteStoredAttemptsForEntry(stores.attempts, entry);
      await requestPromise(stores.entries.delete(id));
      await removeUnusedSigningKey(stores, prepared.scope.key);
      return true;
    },
  );
}

export async function completeEntryAttempt(prepared: PreparedRetry, acknowledgedTerminal = false, requestID?: number) {
  if (!prepared?.attemptID) return false;
  return mutateAfterReleasingAttempt(
    prepared,
    (entries, entry) => {
      if (retiredIdentity(entry, prepared)) {
        cleanupRetiredMemoryEntry(prepared.scope, entries, entry);
        return true;
      }
      if (!entries || !matchingEntry(entry, prepared, acknowledgedTerminal)) return false;
      if (acknowledgedTerminal) assertTerminalRequestIdentity(entry, requestID);
      if (acknowledgedTerminal && entry.state !== "pending") return false;
      if (hasMemoryAttempts(entry.id)) {
        if (!acknowledgedTerminal) return false;
        entries.set(entry.signature, terminalEntry(entry));
        return true;
      }
      entries.delete(prepared.signature);
      if (entries.size === 0) memoryEntries.delete(prepared.scope.key);
      removeUnusedMemorySigningKey(prepared.scope);
      return true;
    },
    async (stores, id, entry) => {
      if (retiredIdentity(entry, prepared)) {
        await cleanupRetiredStoredEntry(stores, prepared.scope, id, entry);
        return true;
      }
      if (!matchingEntry(entry, prepared, acknowledgedTerminal)) return false;
      if (acknowledgedTerminal) assertTerminalRequestIdentity(entry, requestID);
      if (acknowledgedTerminal && entry.state !== "pending") return false;
      if ((await requestPromise(stores.attempts.index("entry_id").count(id))) > 0) {
        if (!acknowledgedTerminal) return false;
        await requestPromise(stores.entries.put(terminalEntry(entry)));
        return true;
      }
      await requestPromise(stores.entries.delete(id));
      await removeUnusedSigningKey(stores, prepared.scope.key);
      return true;
    },
  );
}

export async function deleteEntryIfMatching(scope: RetryScope, signature: string, expectedKey: string, expectedRevision: number) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(scope.key);
      const entry = entries?.get(signature);
      if (!entries || !entry || entry.key !== expectedKey || entry.revision !== expectedRevision) return false;
      if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
      removeExpiredMemoryAttempts();
      if (hasMemoryAttempts(entry.id)) return false;
      entries.delete(signature);
      if (entries.size === 0) memoryEntries.delete(scope.key);
      removeUnusedMemorySigningKey(scope);
      notifyChanged();
      return true;
    });
  }
  const database = await openRetryDatabase();
  const changed = await storesTransactionPromise(
    database,
    [entriesStore, attemptsStore, keysStore, reservationsStore],
    "readwrite",
    async (stores) => {
      const id = entryID(scope.key, signature);
      await removeExpiredAttempts(stores.attempts, Date.now());
      const entry = await readStoredEntry(stores.entries, scope, signature);
      if (!entry || entry.key !== expectedKey || entry.revision !== expectedRevision) return false;
      if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
      if ((await requestPromise(stores.attempts.index("entry_id").count(id))) > 0) return false;
      await requestPromise(stores.entries.delete(id));
      await removeUnusedSigningKey(stores, scope.key);
      return true;
    },
  );
  if (changed) notifyChanged();
  return changed;
}

function reserveMemoryAttempt(scope: RetryScope, entry: RetryEntry, attemptID: string) {
  removeExpiredMemoryAttempts();
  if (memoryAttempts.size >= maxActionAttempts) throw ledgerFullError();
  const attempt = newActionAttempt(scope, entry, attemptID);
  memoryAttempts.set(attempt.id, attempt);
  return { ...attempt };
}

function releaseMemoryAttempt(prepared: PreparedRetry) {
  const attempt = memoryAttempts.get(prepared.attemptID);
  if (attempt === undefined) return false;
  if (!validActionAttempt(attempt, attemptExpectation(prepared))) throw storageError();
  memoryAttempts.delete(prepared.attemptID);
  return true;
}

async function releaseStoredAttempt(store: IDBObjectStore, prepared: PreparedRetry) {
  const attempt = await requestPromise<unknown>(store.get(prepared.attemptID));
  if (attempt === undefined) return false;
  if (!validActionAttempt(attempt, attemptExpectation(prepared))) throw storageError();
  await requestPromise(store.delete(prepared.attemptID));
  return true;
}

function attemptExpectation(prepared: PreparedRetry): AttemptExpectation {
  return {
    id: prepared.attemptID,
    scope: prepared.scope.key,
    entryID: entryID(prepared.scope.key, prepared.signature),
    signature: prepared.signature,
    key: prepared.idempotencyKey,
    revision: prepared.revision,
  };
}

function matchingEntry(entry: RetryEntry | undefined, prepared: RetryIdentity, allowNewerRevision = false): entry is RetryEntry {
  if (!entry || entry.key !== prepared.idempotencyKey) return false;
  if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
  return entry.revision === prepared.revision || (allowNewerRevision && entry.revision > prepared.revision);
}

function terminalEntry(entry: RetryEntry): RetryEntry {
  // Retain the completed identity until overlapping attempts drain; late pending replies must not revive it.
  return { ...entry, state: "retired", revision: entry.revision + 1, updated_at: new Date().toISOString() };
}

function assertTerminalRequestIdentity(entry: RetryEntry, requestID?: number) {
  if (requestID !== undefined && entry.request_id != null && entry.request_id !== requestID) throw retryIdentityChangedError();
}

function retiredIdentity(entry: RetryEntry | undefined, prepared: RetryIdentity) {
  return (
    entry?.state === "retired" && entry.key === prepared.idempotencyKey && validRetryEntry(entry, prepared.scope.key, prepared.signature)
  );
}

async function mutateAfterReleasingAttempt(
  prepared: PreparedRetry,
  mutateMemory: (_entries: RetryEntries | undefined, _entry: RetryEntry | undefined) => boolean,
  mutateStored: (_stores: RetryStores, _id: string, _entry: RetryEntry | undefined) => Promise<boolean>,
) {
  let changed;
  if (!usesIndexedDB()) {
    changed = await withMemoryTransaction(() => {
      releaseMemoryAttempt(prepared);
      removeExpiredMemoryAttempts();
      const entries = memoryEntries.get(prepared.scope.key);
      return mutateMemory(entries, entries?.get(prepared.signature));
    });
  } else {
    const database = await openRetryDatabase();
    changed = await storesTransactionPromise(
      database,
      [entriesStore, attemptsStore, keysStore, reservationsStore],
      "readwrite",
      async (stores) => {
        await releaseStoredAttempt(stores.attempts, prepared);
        await removeExpiredAttempts(stores.attempts, Date.now());
        const id = entryID(prepared.scope.key, prepared.signature);
        const entry = await readStoredEntry(stores.entries, prepared.scope, prepared.signature);
        return mutateStored(stores, id, entry);
      },
    );
  }
  if (changed) notifyChanged();
  return changed;
}

function cleanupRetiredMemoryEntry(scope: RetryScope, entries: RetryEntries | undefined, entry: RetryEntry | undefined) {
  if (!entries || !entry || entry.state !== "retired" || hasMemoryAttempts(entry.id)) return false;
  entries.delete(entry.signature);
  if (entries.size === 0) memoryEntries.delete(scope.key);
  removeUnusedMemorySigningKey(scope);
  return true;
}

async function cleanupRetiredStoredEntry(stores: RetryStores, scope: RetryScope, id: string, entry: RetryEntry | undefined) {
  if (!entry || entry.state !== "retired") return false;
  if ((await requestPromise(stores.attempts.index("entry_id").count(id))) > 0) return false;
  await requestPromise(stores.entries.delete(id));
  await removeUnusedSigningKey(stores, scope.key);
  return true;
}

function removeExpiredMemoryAttempts(now = Date.now()) {
  for (const [id, attempt] of memoryAttempts) {
    if (!validActionAttempt(attempt)) throw storageError();
    if (Date.parse(attempt.expires_at) <= now) memoryAttempts.delete(id);
  }
}

function hasMemoryAttempts(entryIDValue: string) {
  return Array.from(memoryAttempts.values()).some((attempt) => attempt.entry_id === entryIDValue);
}

function deleteMemoryAttemptsForEntry(entry: RetryEntry) {
  for (const [id, attempt] of memoryAttempts) {
    if (attempt.entry_id !== entry.id) continue;
    if (!validEntryAttempt(attempt, entry)) throw storageError();
    memoryAttempts.delete(id);
  }
}

async function deleteStoredAttemptsForEntry(store: IDBObjectStore, entry: RetryEntry) {
  const attempts = await requestPromise<unknown[]>(store.index("entry_id").getAll(entry.id));
  for (const attempt of attempts) {
    if (!validEntryAttempt(attempt, entry)) throw storageError();
    await requestPromise(store.delete(attempt.id));
  }
}

function validEntryAttempt(attempt: unknown, entry: RetryEntry): attempt is ActionAttempt {
  return (
    validActionAttempt(attempt, {
      scope: entry.scope,
      entryID: entry.id,
      signature: entry.signature,
      key: entry.key,
    }) && attempt.revision <= entry.revision
  );
}

async function removeExpiredAttempts(store: IDBObjectStore, now: number) {
  const attempts = await requestPromise<unknown[]>(store.getAll());
  for (const attempt of attempts) {
    if (!validActionAttempt(attempt)) throw storageError();
    if (Date.parse(attempt.expires_at) <= now) await requestPromise(store.delete(attempt.id));
  }
}

export async function allEntries(scope: RetryScope) {
  if (!usesIndexedDB()) {
    const entries = Array.from(memoryEntries.get(scope.key)?.values() || [], (entry) => ({ ...entry }));
    if (entries.some((entry) => !validRetryEntry(entry, scope.key))) throw storageError();
    return entries.filter((entry) => entry.state !== "retired");
  }
  const database = await openRetryDatabase();
  const entries = await requestPromise<unknown[]>(
    database.transaction(entriesStore).objectStore(entriesStore).index("scope").getAll(scope.key),
  );
  if (!entries.every((entry): entry is RetryEntry => validRetryEntry(entry, scope.key))) throw storageError();
  return entries.filter((entry) => entry.state !== "retired");
}

export async function replaceReconciledEntry(scope: RetryScope, expected: RetryEntry) {
  if (!validRetryEntry(expected, scope.key)) throw retryIdentityChangedError();
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(scope.key);
      const current = entries?.get(expected.signature);
      if (!entries || !sameRetryEntry(current, expected)) throw retryIdentityChangedError();
      removeExpiredMemoryAttempts();
      if (hasMemoryAttempts(expected.id)) throw retryIdentityChangedError();
      const replacement = newRetryEntry(scope, expected.signature);
      entries.set(expected.signature, replacement);
      notifyChanged();
      return { ...replacement };
    });
  }
  const database = await openRetryDatabase();
  const replacement = await storesTransactionPromise(database, [entriesStore, attemptsStore], "readwrite", async (stores) => {
    await removeExpiredAttempts(stores.attempts, Date.now());
    const current = await readStoredEntry(stores.entries, scope, expected.signature);
    if (!sameRetryEntry(current, expected)) throw retryIdentityChangedError();
    if ((await requestPromise(stores.attempts.index("entry_id").count(expected.id))) > 0) throw retryIdentityChangedError();
    const next = newRetryEntry(scope, expected.signature);
    await requestPromise(stores.entries.put(next));
    return next;
  });
  notifyChanged();
  return replacement;
}

async function readStoredEntry(store: IDBObjectStore, scope: RetryScope, signature: string): Promise<RetryEntry | undefined> {
  const entry = await requestPromise<unknown>(store.get(entryID(scope.key, signature)));
  if (entry === undefined) return undefined;
  if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
  return entry;
}
