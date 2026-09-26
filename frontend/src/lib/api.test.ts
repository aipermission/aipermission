import assert from "node:assert/strict";
import test from "node:test";
import { IDBFactory, IDBKeyRange } from "fake-indexeddb";

import { apiPost } from "./api.ts";
import {
  completeLocalActionRetry,
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "./local-action-retry.ts";
import { legacyStoragePrefix, localActionReconciliationEvent } from "./local-action-retry/constants.ts";
import { ledgerFullError, retryIdentityChangedError, storageError } from "./local-action-retry/errors.ts";
import { requestReconciliation } from "./local-action-retry/runtime.ts";
import type { ReconciliationDetail } from "./local-action-retry/runtime.ts";
import { resetRetryStorage, transactionPromise } from "./local-action-retry/storage.ts";

const fakeRetryIndexedDB = new IDBFactory();

test("retry helpers ignore absent prepared identities and expose stable errors", async () => {
  assert.equal(await markLocalActionRetryOutcome(null, {}), undefined);
  assert.equal(await completeLocalActionRetry({}), undefined);
  assert.equal(await releaseLocalActionRetryAttempt({ scope: {} }), undefined);
  assert.equal(await preserveLocalActionRetryAttempt({ signature: "missing-scope" }), undefined);
  assert.match(ledgerFullError().message, /ledger is full/i);
  assert.match(storageError().message, /storage is unavailable/i);
  assert.match(retryIdentityChangedError().message, /identity changed/i);
});

test("browser reconciliation only continues after an explicit event decision", async () => {
  const originalWindow = globalThis.window;
  const originalCustomEvent = globalThis.CustomEvent;
  const entry = { request_id: 42, operation_ref: "restore:42", assistant_hint: "Inspect the target", created_at: "2026-09-15" };
  try {
    Reflect.set(globalThis, "window", {
      dispatchEvent(event: CustomEvent<ReconciliationDetail>) {
        assert.equal(event.type, localActionReconciliationEvent);
        assert.equal(event.cancelable, true);
        assert.equal(event.detail.requestID, entry.request_id);
        assert.equal(event.detail.operationRef, entry.operation_ref);
        event.detail.resolve(true);
        event.detail.resolve(false);
        return false;
      },
    });
    assert.equal(await requestReconciliation(entry), true);
    globalThis.window.dispatchEvent = () => true;
    assert.equal(await requestReconciliation(entry), false);
  } finally {
    restoreWindow(originalWindow);
    if (originalCustomEvent === undefined) Reflect.deleteProperty(globalThis, "CustomEvent");
    else globalThis.CustomEvent = originalCustomEvent;
  }
});

test("legacy retry entries remain visible until explicit reconciliation", async () => {
  const workspaceID = "workspace-legacy-ledger";
  const restoreBrowser = installFakeBrowserRetryStorage(workspaceID);
  try {
    globalThis.window.localStorage.setItem(`${legacyStoragePrefix}${workspaceID}`, "protected");
    const [entry] = await listLocalActionRetryEntries();
    assert.equal(entry.signature, "legacy-v2-ledger");
    assert.ok("invalid" in entry);
    assert.equal(entry.invalid, true);
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
    assert.deepEqual(await listLocalActionRetryEntries(), []);
  } finally {
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("retry storage supports the single-store transaction adapter and memory reset", async () => {
  const factory = new IDBFactory();
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = factory.open("single-store-adapter", 1);
    request.onupgradeneeded = () => request.result.createObjectStore("entries");
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  try {
    assert.equal(await transactionPromise(database, "entries", "readonly", (store) => store.name), "entries");
  } finally {
    database.close();
  }

  const originalWindow = globalThis.window;
  Reflect.deleteProperty(globalThis, "window");
  try {
    await resetRetryStorage();
  } finally {
    restoreWindow(originalWindow);
  }
});

test("local connector action retries retain idempotency after uncertain transport failure", async () => {
  const originalFetch = globalThis.fetch;
  const bodies: Record<string, unknown>[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    bodies.push(JSON.parse(String(options?.body)));
    calls += 1;
    if (calls === 1) throw new TypeError("network disconnected");
    return response(localActionResponse(options));
  };
  try {
    const first = { target_ref: "fixture:1:1", action_name: "inspect", input: { b: 2, a: 1 }, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", first), /network disconnected/);
    const reordered = { reason: "test", input: { a: 1, b: 2 }, action_name: "inspect", target_ref: "fixture:1:1" };
    await apiPost("/api/connector-actions/local-run", reordered);
    await apiPost("/api/connector-actions/local-run", first);

    assert.equal(bodies[0].idempotency_key, bodies[1].idempotency_key);
    assert.notEqual(bodies[1].idempotency_key, bodies[2].idempotency_key);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("backup upload retries retain one idempotency identity after response loss", async () => {
  const originalFetch = globalThis.fetch;
  const bodies: Record<string, unknown>[] = [];
  globalThis.fetch = async (_url, options) => {
    bodies.push(JSON.parse(String(options?.body)));
    if (bodies.length === 1) throw new TypeError("backup response lost");
    return response({ id: 7, provider_file_id: "backup-stable" });
  };
  try {
    await assert.rejects(() => apiPost("/api/backup/providers/3/upload", {}), /backup response lost/);
    await apiPost("/api/backup/providers/3/upload", {});
    await apiPost("/api/backup/providers/3/upload", {});
    assert.equal(bodies[0].idempotency_key, bodies[1].idempotency_key);
    assert.notEqual(bodies[1].idempotency_key, bodies[2].idempotency_key);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("bulk command retries retain idempotency across an uncertain response and reload", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-bulk-response-loss");
  const keys: unknown[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) throw new TypeError("response lost");
    return response({
      parallelism: 3,
      items: [{ request_id: 91, target_id: 4, target_name: "host", status: "running" }],
    });
  };
  const body = {
    target_ids: [4],
    command: "hostname",
    reason: "bulk smoke",
    confirmation: "RUN ON 1 TARGETS",
  };
  try {
    await assert.rejects(() => apiPost("/api/console/bulk-exec", body), /response lost/);
    await apiPost("/api/console/bulk-exec", body);
    await apiPost("/api/console/bulk-exec", body);
    assert.equal(keys[0], keys[1]);
    assert.notEqual(keys[1], keys[2]);
  } finally {
    await resetLocalActionRetryLedger();
    globalThis.fetch = originalFetch;
    restoreBrowser();
  }
});

test("local connector action retries retain idempotency after server failures", async () => {
  const originalFetch = globalThis.fetch;
  const keys: unknown[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    return calls === 1 ? response({ error: "gateway failed" }, 502) : response(localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:1:1", action_name: "inspect", input: {}, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /gateway failed/);
    await apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("concurrent local connector action submissions share one retry identity", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-concurrent");
  const keys: unknown[] = [];
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    return response({ error: "gateway failed" }, 502);
  };
  try {
    const body = { target_ref: "fixture:concurrent", action_name: "mutate", input: { value: 1 }, reason: "test" };
    const results = await Promise.allSettled([
      apiPost("/api/connector-actions/local-run", body),
      apiPost("/api/connector-actions/local-run", body),
    ]);
    assert.deepEqual(
      results.map((result) => result.status),
      ["rejected", "rejected"],
    );
    assert.equal(keys.length, 2);
    assert.equal(keys[0], keys[1]);
  } finally {
    globalThis.fetch = originalFetch;
    restoreBrowser();
  }
});

test("a fresh client rejection cannot retire a retry identity used by another active attempt", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-concurrent-rejection");
  const keys: unknown[] = [];
  let releaseFirst: () => void = () => {};
  let releaseSecond: () => void = () => {};
  let signalFirstStarted: () => void = () => {};
  let signalSecondStarted: () => void = () => {};
  const firstGate = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  const firstStarted = new Promise<void>((resolve) => {
    signalFirstStarted = resolve;
  });
  const secondStarted = new Promise<void>((resolve) => {
    signalSecondStarted = resolve;
  });
  const secondGate = new Promise<void>((resolve) => {
    releaseSecond = resolve;
  });
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) {
      signalFirstStarted();
      await firstGate;
      return response({ error: "invalid request" }, 400);
    }
    if (calls === 2) {
      signalSecondStarted();
      await secondGate;
      return response(localActionResponse(options));
    }
    return response(localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:concurrent-rejection", action_name: "mutate", input: {}, reason: "test" };
    const first = apiPost("/api/connector-actions/local-run", body).then(
      () => null,
      (error) => error,
    );
    await firstStarted;
    const second = apiPost("/api/connector-actions/local-run", body);
    await secondStarted;
    releaseFirst();
    const failure: unknown = await first;
    assert.ok(failure instanceof Error);
    assert.match(failure.message, /invalid request/);

    await apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
    assert.equal(keys[1], keys[2]);

    releaseSecond();
    await second;
    await apiPost("/api/connector-actions/local-run", body);
    assert.notEqual(keys[2], keys[3]);
  } finally {
    releaseFirst?.();
    releaseSecond?.();
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("a completed uncertain attempt keeps its retry identity after another attempt is rejected", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-completed-uncertain-attempt");
  const keys: unknown[] = [];
  let releaseFirst: () => void = () => {};
  let signalFirstStarted: () => void = () => {};
  const firstGate = new Promise<void>((resolve) => {
    releaseFirst = resolve;
  });
  const firstStarted = new Promise<void>((resolve) => {
    signalFirstStarted = resolve;
  });
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) {
      signalFirstStarted();
      await firstGate;
      return response({ error: "invalid request" }, 400);
    }
    if (calls === 2) throw new TypeError("network disconnected after dispatch");
    return response(localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:completed-uncertain", action_name: "mutate", input: {}, reason: "test" };
    const first = apiPost("/api/connector-actions/local-run", body).then(
      () => null,
      (error) => error,
    );
    await firstStarted;
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /network disconnected after dispatch/);

    releaseFirst();
    const failure: unknown = await first;
    assert.ok(failure instanceof Error);
    assert.match(failure.message, /invalid request/);

    await apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
    assert.equal(keys[1], keys[2]);

    await apiPost("/api/connector-actions/local-run", body);
    assert.notEqual(keys[2], keys[3]);
  } finally {
    releaseFirst?.();
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("completed retry scopes release signing keys beyond the historical scope limit", async () => {
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-completed-0");
  try {
    await resetLocalActionRetryLedger();
    for (let index = 0; index < 70; index += 1) {
      globalThis.document.cookie = `aipermission_workspace_3210=workspace-completed-${index}`;
      const prepared = await prepareLocalActionRetry({ target_ref: `fixture:${index}`, action_name: "mutate" });
      await completeLocalActionRetry(prepared);
    }
    const records = await readRetryDatabaseRecords();
    assert.equal(records.entries.length, 0);
    assert.equal(records.keys.length, 0);
    assert.equal(records.reservations.length, 0);
    assert.equal(records.attempts.length, 0);
  } finally {
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("retry storage upgrade preserves version-one unresolved identities", async () => {
  const workspaceID = "workspace-version-one";
  const restoreBrowser = installFakeBrowserRetryStorage(workspaceID);
  try {
    await resetLocalActionRetryLedger();
    await seedRetryDatabase(workspaceID, 1);
    const [entry] = await listLocalActionRetryEntries();
    assert.ok("scope" in entry);
    assert.equal(entry.scope, workspaceID);
    assert.equal(entry.state, "pending");
    const records = await readRetryDatabaseRecords();
    assert.equal(records.entries.length, 1);
    assert.equal(records.keys.length, 1);
    assert.equal(records.reservations.length, 0);
    assert.equal(records.attempts.length, 0);
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
  } finally {
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("retry storage upgrade preserves version-two unresolved identities", async () => {
  const workspaceID = "workspace-version-two";
  const restoreBrowser = installFakeBrowserRetryStorage(workspaceID);
  try {
    await resetLocalActionRetryLedger();
    await seedRetryDatabase(workspaceID, 2);
    const [entry] = await listLocalActionRetryEntries();
    assert.ok("scope" in entry);
    assert.equal(entry.scope, workspaceID);
    assert.equal(entry.state, "pending");
    const records = await readRetryDatabaseRecords();
    assert.equal(records.entries.length, 1);
    assert.equal(records.keys.length, 1);
    assert.equal(records.reservations.length, 0);
    assert.equal(records.attempts.length, 0);
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
  } finally {
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("unresolved retry scopes remain protected when signing-key capacity is reclaimed", async () => {
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-protected-0");
  const prepared: Awaited<ReturnType<typeof prepareLocalActionRetry>>[] = [];
  try {
    for (let index = 0; index < 64; index += 1) {
      globalThis.document.cookie = `aipermission_workspace_3210=workspace-protected-${index}`;
      prepared.push(await prepareLocalActionRetry({ target_ref: `fixture:${index}`, action_name: "mutate" }));
    }
    globalThis.document.cookie = "aipermission_workspace_3210=workspace-protected-overflow";
    await assert.rejects(() => prepareLocalActionRetry({ target_ref: "fixture:overflow", action_name: "mutate" }), /retry ledger is full/i);

    await completeLocalActionRetry(prepared[0]);
    const replacement = await prepareLocalActionRetry({ target_ref: "fixture:replacement", action_name: "mutate" });
    const records = await readRetryDatabaseRecords();
    assert.equal(records.keys.length, 64);
    assert.equal(records.entries.length, 64);
    assert.equal(records.reservations.length, 0);
    await completeLocalActionRetry(replacement);
    for (const item of prepared.slice(1)) await completeLocalActionRetry(item);
  } finally {
    globalThis.document.cookie = "aipermission_workspace_3210=workspace-protected-cleanup";
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("a concurrent signing reservation prevents premature key reclamation", async () => {
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-signing-race");
  const originalCrypto = globalThis.crypto;
  let signCalls = 0;
  let releaseSign: () => void = () => {};
  let signalSignStarted: () => void = () => {};
  const signStarted = new Promise<void>((resolve) => {
    signalSignStarted = resolve;
  });
  const signGate = new Promise<void>((resolve) => {
    releaseSign = resolve;
  });
  const subtle = new Proxy(originalCrypto.subtle, {
    get(target, property) {
      if (property === "sign") {
        return async (...args: Parameters<SubtleCrypto["sign"]>) => {
          signCalls += 1;
          if (signCalls === 2) {
            signalSignStarted();
            await signGate;
          }
          return target.sign(...args);
        };
      }
      const value = Reflect.get(target, property, target);
      return typeof value === "function" ? value.bind(target) : value;
    },
  });
  const cryptoWithDelayedSign = new Proxy(originalCrypto, {
    get(target, property) {
      if (property === "subtle") return subtle;
      const value = Reflect.get(target, property, target);
      return typeof value === "function" ? value.bind(target) : value;
    },
  });
  Object.defineProperty(globalThis, "crypto", { configurable: true, value: cryptoWithDelayedSign });
  try {
    const first = await prepareLocalActionRetry({ target_ref: "fixture:first", action_name: "mutate" });
    const secondPromise = prepareLocalActionRetry({ target_ref: "fixture:second", action_name: "mutate" });
    await signStarted;
    await completeLocalActionRetry(first);
    let records = await readRetryDatabaseRecords();
    assert.equal(records.keys.length, 1);
    assert.equal(records.entries.length, 0);
    assert.equal(records.reservations.length, 1);
    assert.equal(records.attempts.length, 0);

    releaseSign();
    const second = await secondPromise;
    await completeLocalActionRetry(second);
    records = await readRetryDatabaseRecords();
    assert.equal(records.keys.length, 0);
    assert.equal(records.entries.length, 0);
    assert.equal(records.reservations.length, 0);
    assert.equal(records.attempts.length, 0);
  } finally {
    Object.defineProperty(globalThis, "crypto", { configurable: true, value: originalCrypto });
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("definitive client rejections do not consume retry ledger capacity", async () => {
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = async () => {
    calls += 1;
    return response({ error: "invalid request" }, 400);
  };
  try {
    for (let index = 0; index < 129; index += 1) {
      await assert.rejects(() =>
        apiPost("/api/connector-actions/local-run", {
          target_ref: `fixture:rejected-${index}`,
          action_name: "mutate",
          input: {},
          reason: "test",
        }),
      );
    }
    assert.equal(calls, 129);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local connector action retains idempotency when a successful body cannot be read", async () => {
  const originalFetch = globalThis.fetch;
  const keys: unknown[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) {
      const result = response({});
      result.text = async () => {
        throw new TypeError("response stream disconnected");
      };
      return result;
    }
    return response(localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:body-read", action_name: "inspect", input: {}, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /response stream disconnected/);
    await apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local connector action retains idempotency after malformed or incomplete success JSON", async () => {
  const originalFetch = globalThis.fetch;
  const keys: unknown[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) return rawResponse('{"status":"completed"');
    if (calls === 2) return response({ status: "completed" });
    return response(localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:body-contract", action_name: "inspect", input: {}, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /Invalid JSON response/);
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /Invalid connector action response/);
    await apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
    assert.equal(keys[1], keys[2]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("caller-provided connector idempotency keys still require a valid action acknowledgement", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => response({ status: "completed" });
  try {
    await assert.rejects(
      () =>
        apiPost("/api/connector-actions/local-run", {
          target_ref: "fixture:caller-key",
          action_name: "inspect",
          input: {},
          reason: "test",
          idempotency_key: "caller-provided-key",
        }),
      /Invalid connector action response/,
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local connector action requires explicit reconciliation after an unknown outcome", async () => {
  const originalFetch = globalThis.fetch;
  const keys: unknown[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    return response(calls === 1 ? localActionResponse(options, "outcome_unknown") : localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:unknown", action_name: "mutate", input: {}, reason: "test" };
    await apiPost("/api/connector-actions/local-run", body);
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /new external attempt was canceled/i);
    assert.equal(keys.length, 1);
    const [entry] = await listLocalActionRetryEntries();
    assert.equal(entry.state, "outcome_unknown");
    assert.equal(await resolveLocalActionRetryEntry(entry), true);
    await apiPost("/api/connector-actions/local-run", body);
    assert.notEqual(keys[0], keys[1]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("local connector action retry keys survive browser reload until acknowledged", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const originalIndexedDB = globalThis.indexedDB;
  const originalIDBKeyRange = globalThis.IDBKeyRange;
  const keys: unknown[] = [];
  const storage = memoryStorage();
  Reflect.set(globalThis, "window", { localStorage: storage, location: { protocol: "http:", port: "3210" } });
  Reflect.set(globalThis, "document", { cookie: "aipermission_workspace_3210=workspace-one" });
  globalThis.indexedDB = fakeRetryIndexedDB;
  globalThis.IDBKeyRange = IDBKeyRange;
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) throw new TypeError("network disconnected");
    return response(localActionResponse(options));
  };
  const body = { target_ref: "fixture:reload", action_name: "inspect", input: { secret: "not-persisted" }, reason: "test" };
  try {
    const firstModule = await import("./api.ts?retry-first");
    await assert.rejects(() => firstModule.apiPost("/api/connector-actions/local-run", body));
    const persisted = await readRetryStoreRecords();
    assert.equal(JSON.stringify(persisted).includes("not-persisted"), false);

    const reloadedModule = await import("./api.ts?retry-reload");
    await reloadedModule.apiPost("/api/connector-actions/local-run", body);
    await reloadedModule.apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
    assert.notEqual(keys[1], keys[2]);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
    restoreDocument(originalDocument);
    restoreGlobal("indexedDB", originalIndexedDB);
    restoreGlobal("IDBKeyRange", originalIDBKeyRange);
  }
});

test("browser retry keys are isolated by persistent workspace identity", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const originalIndexedDB = globalThis.indexedDB;
  const originalIDBKeyRange = globalThis.IDBKeyRange;
  const keys: unknown[] = [];
  const firstWindow = { localStorage: memoryStorage(), location: { protocol: "http:", port: "3210" } };
  const secondWindow = { localStorage: memoryStorage(), location: { protocol: "http:", port: "3210" } };
  Reflect.set(globalThis, "window", firstWindow);
  globalThis.indexedDB = fakeRetryIndexedDB;
  globalThis.IDBKeyRange = IDBKeyRange;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    if (keys.length < 3) throw new TypeError("network disconnected");
    return response(localActionResponse(options));
  };
  const body = { target_ref: "fixture:1:1", action_name: "mutate", input: {}, reason: "test" };
  try {
    Reflect.set(globalThis, "document", { cookie: "aipermission_workspace_3210=workspace-one" });
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
    Reflect.set(globalThis, "window", secondWindow);
    globalThis.document.cookie = "aipermission_workspace_3210=workspace-two";
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
    Reflect.set(globalThis, "window", firstWindow);
    globalThis.document.cookie = "aipermission_workspace_3210=workspace-one";
    await apiPost("/api/connector-actions/local-run", body);
    assert.notEqual(keys[0], keys[1]);
    assert.equal(keys[0], keys[2]);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
    restoreDocument(originalDocument);
    restoreGlobal("indexedDB", originalIndexedDB);
    restoreGlobal("IDBKeyRange", originalIDBKeyRange);
  }
});

test("local connector action fails closed when browser retry storage is unavailable", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  let fetched = false;
  Reflect.set(globalThis, "window", {
    get localStorage() {
      throw new Error("denied");
    },
  });
  globalThis.fetch = async (_url, options) => {
    fetched = true;
    return response(localActionResponse(options));
  };
  try {
    const browserModule = await import("./api.ts?retry-storage-denied");
    await assert.rejects(
      () => browserModule.apiPost("/api/connector-actions/local-run", { target_ref: "fixture:denied", action_name: "inspect" }),
      /retry storage is unavailable/,
    );
    assert.equal(fetched, false);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
  }
});

test("browser connector mutations fail closed when IndexedDB is absent", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const originalIndexedDB = globalThis.indexedDB;
  let fetched = false;
  Reflect.set(globalThis, "window", {
    localStorage: memoryStorage(),
    location: { protocol: "http:", port: "3210" },
    dispatchEvent() {},
  });
  Reflect.set(globalThis, "document", { cookie: "aipermission_workspace_3210=workspace-no-indexeddb" });
  Reflect.deleteProperty(globalThis, "indexedDB");
  globalThis.fetch = async (_url, options) => {
    fetched = true;
    return response(localActionResponse(options));
  };
  try {
    await assert.rejects(
      () => apiPost("/api/connector-actions/local-run", { target_ref: "fixture:no-idb", action_name: "mutate" }),
      /retry storage is unavailable/i,
    );
    assert.equal(fetched, false);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
    restoreDocument(originalDocument);
    restoreGlobal("indexedDB", originalIndexedDB);
  }
});

test("a carried browser retry key survives pre-handler authorization errors", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-auth-retry");
  const keys: unknown[] = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) throw new TypeError("response lost");
    if (calls === 2) return response({ error: "ui session required" }, 401);
    return response(localActionResponse(options));
  };
  try {
    const body = { target_ref: "fixture:auth-retry", action_name: "mutate", input: {}, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /response lost/);
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /ui session required/);
    await apiPost("/api/connector-actions/local-run", body);
    assert.deepEqual(keys, [keys[0], keys[0], keys[0]]);
  } finally {
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("stale browser reconciliation cannot delete a newer retry identity", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-stale-reconcile");
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    calls += 1;
    if (calls === 1) return response(localActionResponse(options, "outcome_unknown"));
    throw new TypeError("response lost");
  };
  try {
    const body = { target_ref: "fixture:stale", action_name: "mutate", input: {}, reason: "test" };
    await apiPost("/api/connector-actions/local-run", body);
    const [stale] = await listLocalActionRetryEntries();
    assert.equal(await resolveLocalActionRetryEntry(stale), true);
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /response lost/);
    assert.equal(await resolveLocalActionRetryEntry(stale), false);
    const [current] = await listLocalActionRetryEntries();
    assert.notEqual(current.key, stale.key);
  } finally {
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("missing browser signing key with unresolved entries fails closed", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-missing-key");
  let calls = 0;
  globalThis.fetch = async () => {
    calls += 1;
    throw new TypeError("response lost");
  };
  try {
    const body = { target_ref: "fixture:key-loss", action_name: "mutate", input: {}, reason: "test" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /response lost/);
    await deleteRetrySigningKey("workspace-missing-key");
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body), /retry storage is unavailable/i);
    assert.equal(calls, 1);
    await resetLocalActionRetryLedger();
    globalThis.fetch = async (_url, options) => response(localActionResponse(options));
    await apiPost("/api/connector-actions/local-run", body);
  } finally {
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

function response(body: unknown, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(body), { status });
}

function rawResponse(body: string, status = 200) {
  return new Response(body, { status });
}

async function readRetryStoreRecords() {
  return (await readRetryDatabaseRecords()).entries;
}

async function seedRetryDatabase(scope: string, version: number) {
  const signature = "a".repeat(64);
  const key = await globalThis.crypto.subtle.generateKey({ name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = globalThis.indexedDB.open("aipermission-local-action-retry", version);
    request.onupgradeneeded = () => {
      const entries = request.result.createObjectStore("entries", { keyPath: "id" });
      entries.createIndex("scope", "scope", { unique: false });
      request.result.createObjectStore("keys", { keyPath: "scope" });
      if (version >= 2) {
        const reservations = request.result.createObjectStore("reservations", { keyPath: "id" });
        reservations.createIndex("scope", "scope", { unique: false });
        reservations.createIndex("expires_at", "expires_at", { unique: false });
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction(["entries", "keys"], "readwrite");
      const now = new Date().toISOString();
      transaction.objectStore("entries").add({
        id: `${scope}:${signature}`,
        scope,
        signature,
        key: "version-one-idempotency-key",
        state: "pending",
        revision: 1,
        created_at: now,
        updated_at: now,
      });
      transaction.objectStore("keys").add({ scope, key, updated_at: now });
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error);
      transaction.onabort = () => reject(transaction.error);
    });
  } finally {
    database.close();
  }
}

async function readRetryDatabaseRecords() {
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = globalThis.indexedDB.open("aipermission-local-action-retry", 3);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  try {
    const transaction = database.transaction(["entries", "keys", "reservations", "attempts"]);
    const readAll = (storeName: string) =>
      new Promise<unknown[]>((resolve, reject) => {
        const request = transaction.objectStore(storeName).getAll();
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
    const [entries, keys, reservations, attempts] = await Promise.all([
      readAll("entries"),
      readAll("keys"),
      readAll("reservations"),
      readAll("attempts"),
    ]);
    return { entries, keys, reservations, attempts };
  } finally {
    database.close();
  }
}

async function deleteRetrySigningKey(scope: string) {
  const database = await new Promise<IDBDatabase>((resolve, reject) => {
    const request = globalThis.indexedDB.open("aipermission-local-action-retry", 3);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  try {
    await new Promise<void>((resolve, reject) => {
      const transaction = database.transaction("keys", "readwrite");
      transaction.objectStore("keys").delete(scope);
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error);
    });
  } finally {
    database.close();
  }
}

function restoreGlobal(name: string, value: unknown) {
  if (value === undefined) Reflect.deleteProperty(globalThis, name);
  else Reflect.set(globalThis, name, value);
}

function installFakeBrowserRetryStorage(workspaceID: string) {
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const originalIndexedDB = globalThis.indexedDB;
  const originalIDBKeyRange = globalThis.IDBKeyRange;
  Reflect.set(globalThis, "window", {
    localStorage: memoryStorage(),
    location: { protocol: "http:", port: "3210" },
    dispatchEvent() {},
  });
  Reflect.set(globalThis, "document", { cookie: `aipermission_workspace_3210=${workspaceID}` });
  globalThis.indexedDB = fakeRetryIndexedDB;
  globalThis.IDBKeyRange = IDBKeyRange;
  return () => {
    restoreWindow(originalWindow);
    restoreDocument(originalDocument);
    restoreGlobal("indexedDB", originalIndexedDB);
    restoreGlobal("IDBKeyRange", originalIDBKeyRange);
  };
}

function localActionResponse(options: RequestInit | undefined, status = "completed") {
  const body = JSON.parse(String(options?.body));
  return {
    request_id: 41,
    status,
    target_ref: body.target_ref,
    connector_kind: "fixture",
    action_name: body.action_name,
    retry_policy: { class: "non_idempotent", guidance: "Inspect state before retrying." },
  };
}

function restoreWindow(value: unknown) {
  restoreGlobal("window", value);
}

function restoreDocument(value: unknown) {
  restoreGlobal("document", value);
}

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem(key: string) {
      return values.has(key) ? values.get(key) : null;
    },
    setItem(key: string, value: string) {
      values.set(key, String(value));
    },
    removeItem(key: string) {
      values.delete(key);
    },
    values() {
      return values.values();
    },
  };
}
