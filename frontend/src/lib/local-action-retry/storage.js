import { attemptsStore, databaseName, databaseVersion, entriesStore, keysStore, reservationsStore } from "./constants.js";
import { storageError } from "./errors.js";
import { validRetryDatabaseSchema } from "./records.js";
import { isBrowserRuntime, requireBrowserIndexedDB } from "./runtime.js";

export const memoryEntries = new Map();
export const memoryKeys = new Map();
export const memoryReservations = new Map();
export const memoryAttempts = new Map();

let memoryQueue = Promise.resolve();
let retryDatabasePromise;
let retryDatabase;

export function openRetryDatabase() {
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
      if (!database.objectStoreNames.contains(attemptsStore)) {
        const store = database.createObjectStore(attemptsStore, { keyPath: "id" });
        store.createIndex("scope", "scope", { unique: false });
        store.createIndex("entry_id", "entry_id", { unique: false });
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

export function transactionPromise(database, storeName, mode, operation) {
  return storesTransactionPromise(database, [storeName], mode, (stores) => operation(stores[storeName]));
}

export function storesTransactionPromise(database, storeNames, mode, operation) {
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

export function requestPromise(request) {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || storageError());
  });
}

export function withMemoryTransaction(operation) {
  const next = memoryQueue.then(operation, operation);
  memoryQueue = next.catch(() => {});
  return next;
}

export async function resetRetryStorage() {
  if (isBrowserRuntime()) {
    requireBrowserIndexedDB();
    if (retryDatabase) retryDatabase.close();
    retryDatabase = undefined;
    retryDatabasePromise = undefined;
    await deleteRetryDatabase();
    return;
  }
  await withMemoryTransaction(() => {
    memoryEntries.clear();
    memoryKeys.clear();
    memoryReservations.clear();
    memoryAttempts.clear();
  });
}

function deleteRetryDatabase() {
  return new Promise((resolve, reject) => {
    const request = globalThis.indexedDB.deleteDatabase(databaseName);
    request.onsuccess = () => resolve();
    request.onerror = () => reject(request.error || storageError());
    request.onblocked = () => reject(storageError());
  });
}
