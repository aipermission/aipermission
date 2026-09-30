import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { IDBFactory } from "fake-indexeddb";
import {
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  releaseLocalActionRetryAttempt,
  resolveLocalActionRetryEntry,
} from "../local-action-retry.ts";
import { connectorApprovalFixture } from "../../test/connector-action-fixtures.ts";
import { getEntry, replaceReconciledEntry } from "./entries.ts";
import {
  reconciledRequests,
  requestWasReconciled,
  forgetDefinitiveReconciliations,
  reconcileVerifiedServerRequest,
} from "./reconciliations.ts";
import { databaseName, databaseVersion, maxReconciliations, reconciliationsStore } from "./constants.ts";
import { memoryReconciliations, openRetryDatabase, requestPromise, resetRetryStorage, transactionPromise } from "./storage.ts";
import type { RetryEntry } from "./records.ts";
import { newRetryEntry, newActionAttempt, newSigningReservation } from "./records.ts";

const scope = { key: "non-browser" };
const request = {
  path: "/api/connector-actions/local-run",
  body: {
    target_ref: "example:1:1",
    action_name: "mutate",
    input: { value: "private-payload-canary" },
    reason: "reconciliation control",
  },
};
const approval = (status = "outcome_unknown") =>
  connectorApprovalFixture({
    id: 71,
    target_ref: request.body.target_ref,
    action_name: request.body.action_name,
    status: status as ReturnType<typeof connectorApprovalFixture>["status"],
  });
afterEach(async () => {
  await resetRetryStorage();
  Reflect.deleteProperty(globalThis, "window");
  Reflect.deleteProperty(globalThis, "indexedDB");
});

function useAdapter(adapter: string) {
  if (adapter === "memory") return;
  Object.defineProperty(globalThis, "window", { value: {}, configurable: true });
  Object.defineProperty(globalThis, "indexedDB", { value: new IDBFactory(), configurable: true });
}

async function unknownEntry() {
  const prepared = await prepareLocalActionRetry(request, { workspaceID: scope.key });
  await markLocalActionRetryOutcome(prepared, { request_id: 71, status: "outcome_unknown" });
  const [entry] = await listLocalActionRetryEntries();
  return { prepared, entry: entry as RetryEntry };
}

for (const adapter of ["memory", "indexeddb"]) {
  test(`${adapter} reconciles a verified server-only identity without binding or deleting unidentified local retries`, async () => {
    useAdapter(adapter);
    const prepared = await prepareLocalActionRetry(request, { workspaceID: scope.key });
    await markLocalActionRetryOutcome(prepared, { status: "outcome_unknown" });
    const [entry] = (await listLocalActionRetryEntries()) as RetryEntry[];
    assert.equal(entry.request_id, undefined);
    await reconcileVerifiedServerRequest(scope, approval());
    assert.deepEqual(await getEntry(scope, entry.signature), entry);
    assert.equal(requestWasReconciled(await reconciledRequests(scope), approval()), true);
    for (const status of ["running", "approval_pending", "completed", "failed"])
      await assert.rejects(reconcileVerifiedServerRequest(scope, approval(status)), /Only a verified unknown/);
    await assert.rejects(reconcileVerifiedServerRequest(scope, { ...approval(), id: 0 }), /Invalid/);
    assert.equal((await reconciledRequests(scope)).length, 1);
    assert.deepEqual(await reconciledRequests({ key: "other-workspace" }), []);
  });

  test(`${adapter} persists only exact explicit reconciliation metadata and never reuses it across identities`, async () => {
    useAdapter(adapter);
    const { entry } = await unknownEntry();
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
    const records = await reconciledRequests(scope);
    assert.equal(requestWasReconciled(records, approval()), true);
    for (const item of [
      { ...approval(), id: 72 },
      { ...approval(), target_ref: "example:1:2" },
      { ...approval(), action_name: "other_action" },
    ])
      assert.equal(requestWasReconciled(records, item), false);
    assert.deepEqual(await reconciledRequests({ key: "other-workspace" }), []);
    assert.equal(JSON.stringify(records).includes("private-payload-canary"), false);
    assert.equal(JSON.stringify(records).includes(entry.key), false);
    assert.equal(await resolveLocalActionRetryEntry(entry), false);
    await forgetDefinitiveReconciliations(scope, [approval("running"), approval()]);
    assert.equal((await reconciledRequests(scope)).length, 1);
    await forgetDefinitiveReconciliations(scope, [{ ...approval("completed"), id: 72 }]);
    assert.equal((await reconciledRequests(scope)).length, 1);
    await forgetDefinitiveReconciliations(scope, [approval("completed")]);
    assert.deepEqual(await reconciledRequests(scope), []);
  });

  test(`${adapter} rejects active and stale reconciliation before committing any proof`, async () => {
    useAdapter(adapter);
    const first = await prepareLocalActionRetry(request, { workspaceID: scope.key });
    const overlapping = await prepareLocalActionRetry(request, { workspaceID: scope.key });
    await markLocalActionRetryOutcome(first, { request_id: 71 });
    const [entry] = (await listLocalActionRetryEntries()) as RetryEntry[];
    assert.equal(await resolveLocalActionRetryEntry(entry), false);
    assert.deepEqual(await reconciledRequests(scope), []);
    await releaseLocalActionRetryAttempt(overlapping);
    assert.equal(await resolveLocalActionRetryEntry({ ...entry, revision: entry.revision + 1 }), false);
    assert.deepEqual(await reconciledRequests(scope), []);
    assert.equal(await resolveLocalActionRetryEntry({ ...entry, target_ref: "example:9:9" }), true);
    assert.equal(requestWasReconciled(await reconciledRequests(scope), approval()), true);
    assert.equal(requestWasReconciled(await reconciledRequests(scope), { ...approval(), target_ref: "example:9:9" }), false);
  });

  test(`${adapter} records an explicitly replaced identity without authorizing the replacement implicitly`, async () => {
    useAdapter(adapter);
    const { entry } = await unknownEntry();
    const next = await replaceReconciledEntry(scope, entry);
    assert.notEqual(next.key, entry.key);
    assert.equal(requestWasReconciled(await reconciledRequests(scope), approval()), true);
    assert.equal((await getEntry(scope, next.signature))?.state, "pending");
  });

  test(`${adapter} fails closed on capacity and corruption without deleting the protected entry`, async () => {
    useAdapter(adapter);
    const { entry } = await unknownEntry();
    const filler = Array.from({ length: maxReconciliations }, (_, index) => {
      const requestID = index + 100;
      return {
        id: JSON.stringify([scope.key, requestID, "example:1:1", "mutate"]),
        scope: scope.key,
        request_id: requestID,
        target_ref: "example:1:1",
        action_name: "mutate",
      };
    });
    if (adapter === "memory") for (const record of filler) memoryReconciliations.set(record.id, record);
    else
      await transactionPromise(await openRetryDatabase(), reconciliationsStore, "readwrite", async (store) => {
        for (const record of filler) await requestPromise(store.add(record));
      });
    await assert.rejects(resolveLocalActionRetryEntry(entry), /storage is full/);
    assert.equal((await getEntry(scope, entry.signature))?.key, entry.key);
    assert.equal((await reconciledRequests(scope)).length, maxReconciliations);
    const corrupt = { ...filler[0], request_id: -1 };
    if (adapter === "memory") memoryReconciliations.set(corrupt.id, corrupt);
    else
      await transactionPromise(await openRetryDatabase(), reconciliationsStore, "readwrite", (store) => requestPromise(store.put(corrupt)));
    await assert.rejects(reconciledRequests(scope), /Secure retry storage/);
  });
}

test("an unattributed or console-batch identity never creates a wildcard reconciliation", async () => {
  for (const request of [{ old_shape: "unattributed" }, { path: "/api/console/bulk-exec", body: {} }]) {
    const prepared = await prepareLocalActionRetry(request);
    await markLocalActionRetryOutcome(prepared, { request_id: 71 });
    const [entry] = await listLocalActionRetryEntries();
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
    assert.deepEqual(await reconciledRequests(scope), []);
  }
});

async function fixtureDatabase(
  version: number,
  proofOptions: IDBObjectStoreParameters = { keyPath: "id" },
  indexPath = "scope",
  indexOptions: IDBIndexParameters = {},
) {
  return new Promise<IDBDatabase>((resolve, reject) => {
    const request = globalThis.indexedDB.open(databaseName, version);
    request.onupgradeneeded = () => {
      const entries = request.result.createObjectStore("entries", { keyPath: "id" });
      entries.createIndex("scope", "scope");
      request.result.createObjectStore("keys", { keyPath: "scope" });
      for (const name of ["reservations", "attempts"]) {
        const store = request.result.createObjectStore(name, { keyPath: "id" });
        store.createIndex("scope", "scope");
        store.createIndex("expires_at", "expires_at");
        if (name === "attempts") store.createIndex("entry_id", "entry_id");
      }
      if (version >= 4) request.result.createObjectStore(reconciliationsStore, proofOptions).createIndex("scope", indexPath, indexOptions);
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
}

test("v3 upgrade preserves unresolved entries, signing reservations and active attempts", async () => {
  useAdapter("indexeddb");
  const database = await fixtureDatabase(3);
  const entry = {
    ...newRetryEntry(scope, "a".repeat(64)),
    state: "outcome_unknown" as const,
    request_id: 71,
    target_ref: request.body.target_ref,
    action_name: request.body.action_name,
  };
  const reservation = newSigningReservation(scope);
  const attempt = newActionAttempt(scope, entry, reservation.id);
  const key = await crypto.subtle.generateKey({ name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  await new Promise<void>((resolve, reject) => {
    const transaction = database.transaction(["entries", "keys", "reservations", "attempts"], "readwrite");
    transaction.objectStore("entries").put(entry);
    transaction.objectStore("keys").put({ scope: scope.key, key });
    transaction.objectStore("reservations").put(reservation);
    transaction.objectStore("attempts").put(attempt);
    transaction.oncomplete = () => resolve();
    transaction.onabort = () => reject(transaction.error);
  });
  database.close();
  const upgraded = await openRetryDatabase();
  assert.equal(upgraded.version, databaseVersion);
  assert.deepEqual(await getEntry(scope, entry.signature), entry);
  for (const [store, id, expected] of [
    ["reservations", reservation.id, reservation],
    ["attempts", attempt.id, attempt],
  ] as const)
    assert.deepEqual(await requestPromise(upgraded.transaction(store).objectStore(store).get(id)), expected);
  assert.equal(await resolveLocalActionRetryEntry(entry), false);
  assert.deepEqual(await reconciledRequests(scope), []);
  await releaseLocalActionRetryAttempt({
    scope,
    signature: entry.signature,
    idempotencyKey: entry.key,
    revision: entry.revision,
    attemptID: attempt.id,
    reused: false,
  });
  assert.equal(await resolveLocalActionRetryEntry(entry), true);
});

test("an IndexedDB quota exception atomically preserves the entry and signing key", async (t) => {
  useAdapter("indexeddb");
  const { entry } = await unknownEntry();
  const database = await openRetryDatabase();
  const prototype = Object.getPrototypeOf(database.transaction(reconciliationsStore).objectStore(reconciliationsStore));
  const original = prototype.put;
  t.mock.method(prototype, "put", function (this: IDBObjectStore, ...args: unknown[]) {
    if (this.name === reconciliationsStore) throw new DOMException("quota fixture", "QuotaExceededError");
    return original.apply(this, args);
  });
  await assert.rejects(resolveLocalActionRetryEntry(entry), { name: "QuotaExceededError" });
  assert.equal((await getEntry(scope, entry.signature))?.key, entry.key);
  assert.deepEqual(await reconciledRequests(scope), []);
  assert.ok(await requestPromise(database.transaction("keys").objectStore("keys").get(scope.key)));
});

for (const [name, options, indexPath, indexOptions] of [
  ["wrong key path", { keyPath: "scope" }, "scope", {}],
  ["auto increment", { keyPath: "id", autoIncrement: true }, "scope", {}],
  ["wrong index", { keyPath: "id" }, "request_id", {}],
  ["unique index", { keyPath: "id" }, "scope", { unique: true }],
  ["multi-entry index", { keyPath: "id" }, "scope", { multiEntry: true }],
] as const) {
  test(`malformed reconciliation ${name} refuses storage without changing existing entries`, async () => {
    useAdapter("indexeddb");
    const database = await fixtureDatabase(databaseVersion, options, indexPath, indexOptions);
    const entry = newRetryEntry(scope, "a".repeat(64));
    await transactionPromise(database, "entries", "readwrite", (store) => requestPromise(store.put(entry)));
    database.close();
    await assert.rejects(openRetryDatabase(), /Secure retry storage/);
    const raw = await fixtureDatabase(databaseVersion);
    assert.deepEqual(await requestPromise(raw.transaction("entries").objectStore("entries").get(entry.id)), entry);
    raw.close();
  });
}
