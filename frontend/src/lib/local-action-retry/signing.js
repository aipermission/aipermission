import { entriesStore, keysStore, maxRetryScopes, reservationsStore } from "./constants.js";
import { ledgerFullError, retryIdentityChangedError, storageError } from "./errors.js";
import { newSigningReservation, validSigningKeyRecord, validSigningReservation } from "./records.js";
import { usesIndexedDB } from "./runtime.js";
import {
  memoryEntries,
  memoryKeys,
  memoryReservations,
  openRetryDatabase,
  requestPromise,
  storesTransactionPromise,
  withMemoryTransaction,
} from "./storage.js";

export async function reserveSigningKey(scope) {
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

export async function releaseSigningReservation(scope, reservationID) {
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

export async function requireSigningReservation(store, scope, reservationID) {
  const reservation = await requestPromise(store.get(reservationID));
  if (!validSigningReservation(reservation, scope.key, reservationID) || Date.parse(reservation.expires_at) <= Date.now()) {
    throw retryIdentityChangedError();
  }
}

export function requireMemorySigningReservation(scope, reservationID) {
  const reservation = memoryReservations.get(scope.key)?.get(reservationID);
  if (!validSigningReservation(reservation, scope.key, reservationID) || Date.parse(reservation.expires_at) <= Date.now()) {
    removeMemorySigningReservation(scope, reservationID);
    removeUnusedMemorySigningKey(scope);
    throw retryIdentityChangedError();
  }
}

export function removeMemorySigningReservation(scope, reservationID) {
  const reservations = memoryReservations.get(scope.key);
  reservations?.delete(reservationID);
  if (reservations?.size === 0) memoryReservations.delete(scope.key);
}

export function removeUnusedMemorySigningKey(scope) {
  if ((memoryEntries.get(scope.key)?.size || 0) > 0) return;
  if ((memoryReservations.get(scope.key)?.size || 0) > 0) return;
  memoryKeys.delete(scope.key);
}

export async function removeUnusedSigningKey(stores, scope) {
  const entryCount = await requestPromise(stores.entries.index("scope").count(scope));
  if (entryCount > 0) return false;
  const reservationCount = await requestPromise(stores.reservations.index("scope").count(scope));
  if (reservationCount > 0) return false;
  await requestPromise(stores.keys.delete(scope));
  return true;
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
