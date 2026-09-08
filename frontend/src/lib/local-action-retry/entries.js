import { attemptsStore, entriesStore, keysStore, maxActionAttempts, maxEntries, maxGlobalEntries, reservationsStore } from "./constants.js";
import { ledgerFullError, retryIdentityChangedError, storageError } from "./errors.js";
import { entryID, newActionAttempt, newRetryEntry, sameRetryEntry, validActionAttempt, validRetryEntry } from "./records.js";
import { notifyChanged, usesIndexedDB } from "./runtime.js";
import {
  requireMemorySigningReservation,
  requireSigningReservation,
  removeMemorySigningReservation,
  removeUnusedMemorySigningKey,
  removeUnusedSigningKey,
} from "./signing.js";
import {
  memoryEntries,
  memoryAttempts,
  openRetryDatabase,
  requestPromise,
  storesTransactionPromise,
  transactionPromise,
  withMemoryTransaction,
} from "./storage.js";

export async function reserveEntry(scope, signature, reservationID) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      requireMemorySigningReservation(scope, reservationID);
      const entries = memoryEntries.get(scope.key) || new Map();
      let entry = entries.get(signature);
      if (entry) {
        if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
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
      let entry = await requestPromise(stores.entries.get(id));
      if (entry) {
        if (!validRetryEntry(entry, scope.key, signature)) throw storageError();
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

export async function getEntry(scope, signature) {
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

export async function updateEntryIfMatching(prepared, update) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(prepared.scope.key);
      const entry = entries?.get(prepared.signature);
      releaseMemoryAttempt(prepared);
      if (!entry || entry.key !== prepared.idempotencyKey || entry.revision !== prepared.revision) return false;
      if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
      entries.set(prepared.signature, update(entry));
      notifyChanged();
      return true;
    });
  }
  const database = await openRetryDatabase();
  const changed = await storesTransactionPromise(database, [entriesStore, attemptsStore], "readwrite", async (stores) => {
    const id = entryID(prepared.scope.key, prepared.signature);
    await releaseStoredAttempt(stores.attempts, prepared);
    const entry = await requestPromise(stores.entries.get(id));
    if (!entry || entry.key !== prepared.idempotencyKey || entry.revision !== prepared.revision) return false;
    if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
    await requestPromise(stores.entries.put(update(entry)));
    return true;
  });
  if (changed) notifyChanged();
  return changed;
}

export async function releaseEntryAttempt(prepared) {
  if (!prepared?.attemptID) return;
  if (!usesIndexedDB()) {
    await withMemoryTransaction(() => releaseMemoryAttempt(prepared));
    return;
  }
  const database = await openRetryDatabase();
  await transactionPromise(database, attemptsStore, "readwrite", (store) => releaseStoredAttempt(store, prepared));
}

export async function completeEntryAttempt(prepared) {
  if (!prepared?.attemptID) return false;
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      releaseMemoryAttempt(prepared);
      const entries = memoryEntries.get(prepared.scope.key);
      const entry = entries?.get(prepared.signature);
      if (!matchingEntry(entry, prepared)) return false;
      removeExpiredMemoryAttempts();
      if (hasMemoryAttempts(entry.id)) return false;
      entries.delete(prepared.signature);
      if (entries.size === 0) memoryEntries.delete(prepared.scope.key);
      removeUnusedMemorySigningKey(prepared.scope);
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
      await releaseStoredAttempt(stores.attempts, prepared);
      await removeExpiredAttempts(stores.attempts, Date.now());
      const id = entryID(prepared.scope.key, prepared.signature);
      const entry = await requestPromise(stores.entries.get(id));
      if (!matchingEntry(entry, prepared)) return false;
      if ((await requestPromise(stores.attempts.index("entry_id").count(id))) > 0) return false;
      await requestPromise(stores.entries.delete(id));
      await removeUnusedSigningKey(stores, prepared.scope.key);
      return true;
    },
  );
  if (changed) notifyChanged();
  return changed;
}

export async function deleteEntryIfMatching(scope, signature, expectedKey, expectedRevision) {
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(scope.key);
      const entry = entries?.get(signature);
      if (!entry || entry.key !== expectedKey || entry.revision !== expectedRevision) return false;
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
      const entry = await requestPromise(stores.entries.get(id));
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

function reserveMemoryAttempt(scope, entry, attemptID) {
  removeExpiredMemoryAttempts();
  if (memoryAttempts.size >= maxActionAttempts) throw ledgerFullError();
  const attempt = newActionAttempt(scope, entry, attemptID);
  memoryAttempts.set(attempt.id, attempt);
  return { ...attempt };
}

function releaseMemoryAttempt(prepared) {
  const attempt = memoryAttempts.get(prepared.attemptID);
  if (attempt === undefined) return false;
  if (!validActionAttempt(attempt, attemptExpectation(prepared))) throw storageError();
  memoryAttempts.delete(prepared.attemptID);
  return true;
}

async function releaseStoredAttempt(store, prepared) {
  const attempt = await requestPromise(store.get(prepared.attemptID));
  if (attempt === undefined) return false;
  if (!validActionAttempt(attempt, attemptExpectation(prepared))) throw storageError();
  await requestPromise(store.delete(prepared.attemptID));
  return true;
}

function attemptExpectation(prepared) {
  return {
    id: prepared.attemptID,
    scope: prepared.scope.key,
    entryID: entryID(prepared.scope.key, prepared.signature),
    signature: prepared.signature,
    key: prepared.idempotencyKey,
    revision: prepared.revision,
  };
}

function matchingEntry(entry, prepared) {
  if (!entry || entry.key !== prepared.idempotencyKey || entry.revision !== prepared.revision) return false;
  if (!validRetryEntry(entry, prepared.scope.key, prepared.signature)) throw storageError();
  return true;
}

function removeExpiredMemoryAttempts(now = Date.now()) {
  for (const [id, attempt] of memoryAttempts) {
    if (!validActionAttempt(attempt)) throw storageError();
    if (Date.parse(attempt.expires_at) <= now) memoryAttempts.delete(id);
  }
}

function hasMemoryAttempts(entryIDValue) {
  return Array.from(memoryAttempts.values()).some((attempt) => attempt.entry_id === entryIDValue);
}

async function removeExpiredAttempts(store, now) {
  const attempts = await requestPromise(store.getAll());
  for (const attempt of attempts) {
    if (!validActionAttempt(attempt)) throw storageError();
    if (Date.parse(attempt.expires_at) <= now) await requestPromise(store.delete(attempt.id));
  }
}

export async function allEntries(scope) {
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

export async function replaceReconciledEntry(scope, expected) {
  if (!validRetryEntry(expected, scope.key)) throw retryIdentityChangedError();
  if (!usesIndexedDB()) {
    return withMemoryTransaction(() => {
      const entries = memoryEntries.get(scope.key);
      const current = entries?.get(expected.signature);
      if (!sameRetryEntry(current, expected)) throw retryIdentityChangedError();
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
    const current = await requestPromise(stores.entries.get(expected.id));
    if (!sameRetryEntry(current, expected)) throw retryIdentityChangedError();
    if ((await requestPromise(stores.attempts.index("entry_id").count(expected.id))) > 0) throw retryIdentityChangedError();
    const next = newRetryEntry(scope, expected.signature);
    await requestPromise(stores.entries.put(next));
    return next;
  });
  notifyChanged();
  return replacement;
}
