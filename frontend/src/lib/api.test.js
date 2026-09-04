import assert from "node:assert/strict";
import test from "node:test";
import { IDBFactory, IDBKeyRange } from "fake-indexeddb";

import { apiDownload, apiPost } from "./api.js";
import { listLocalActionRetryEntries, resetLocalActionRetryLedger, resolveLocalActionRetryEntry } from "./local-action-retry.js";

const fakeRetryIndexedDB = new IDBFactory();

test("picker downloads stream the response directly to the selected file", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const writable = {};
  let destination = null;
  let suggestedName = "";
  globalThis.window = {
    showSaveFilePicker: async ({ suggestedName: value }) => {
      suggestedName = value;
      return { createWritable: async () => writable };
    },
  };
  globalThis.fetch = async () => ({
    ok: true,
    body: {
      async pipeTo(value) {
        destination = value;
      },
    },
    async blob() {
      throw new Error("streaming download must not buffer a Blob");
    },
  });
  try {
    const result = await apiDownload("/api/file-transfer-batches/1/download", "backup:latest.zip", { picker: true });
    assert.deepEqual(result, { saved: true, method: "picker" });
    assert.equal(suggestedName, "backup-latest.zip");
    assert.equal(destination, writable);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
  }
});

test("picker downloads retain the Blob fallback when response streaming is unavailable", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const blob = { size: 42 };
  const writes = [];
  let closed = false;
  globalThis.window = {
    showSaveFilePicker: async () => ({
      createWritable: async () => ({
        async write(value) {
          writes.push(value);
        },
        async close() {
          closed = true;
        },
      }),
    }),
  };
  globalThis.fetch = async () => ({ ok: true, body: null, blob: async () => blob });
  try {
    const result = await apiDownload("/api/backup/download", "backup.aipdb", { picker: true });
    assert.deepEqual(result, { saved: true, method: "picker" });
    assert.deepEqual(writes, [blob]);
    assert.equal(closed, true);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
  }
});

test("local connector action retries retain idempotency after uncertain transport failure", async () => {
  const originalFetch = globalThis.fetch;
  const bodies = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    bodies.push(JSON.parse(options.body));
    calls += 1;
    if (calls === 1) throw new TypeError("network disconnected");
    return response(localActionResponse());
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

test("local connector action retries retain idempotency after server failures", async () => {
  const originalFetch = globalThis.fetch;
  const keys = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    return calls === 1 ? response({ error: "gateway failed" }, 502) : response(localActionResponse());
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
  const keys = [];
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
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
  const keys = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    if (calls === 1) {
      return {
        ok: true,
        status: 200,
        async text() {
          throw new TypeError("response stream disconnected");
        },
      };
    }
    return response(localActionResponse());
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
  const keys = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    if (calls === 1) return rawResponse('{"status":"completed"');
    if (calls === 2) return response({ status: "completed" });
    return response(localActionResponse());
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

test("local connector action requires explicit reconciliation after an unknown outcome", async () => {
  const originalFetch = globalThis.fetch;
  const keys = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    return response(calls === 1 ? localActionResponse("outcome_unknown") : localActionResponse());
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
  const keys = [];
  const storage = memoryStorage();
  globalThis.window = { localStorage: storage, location: { protocol: "http:", port: "3210" } };
  globalThis.document = { cookie: "aipermission_workspace_3210=workspace-one" };
  globalThis.indexedDB = fakeRetryIndexedDB;
  globalThis.IDBKeyRange = IDBKeyRange;
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    if (calls === 1) throw new TypeError("network disconnected");
    return response(localActionResponse());
  };
  const body = { target_ref: "fixture:reload", action_name: "inspect", input: { secret: "not-persisted" }, reason: "test" };
  try {
    const firstModule = await import(`./api.js?retry-first=${Date.now()}`);
    await assert.rejects(() => firstModule.apiPost("/api/connector-actions/local-run", body));
    const persisted = await readRetryStoreRecords();
    assert.equal(JSON.stringify(persisted).includes("not-persisted"), false);

    const reloadedModule = await import(`./api.js?retry-reload=${Date.now()}`);
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
  const keys = [];
  const storage = memoryStorage();
  globalThis.window = { localStorage: storage, location: { protocol: "http:", port: "3210" } };
  globalThis.indexedDB = fakeRetryIndexedDB;
  globalThis.IDBKeyRange = IDBKeyRange;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    if (keys.length < 3) throw new TypeError("network disconnected");
    return response(localActionResponse());
  };
  const body = { target_ref: "fixture:1:1", action_name: "mutate", input: {}, reason: "test" };
  try {
    globalThis.document = { cookie: "aipermission_workspace_3210=workspace-one" };
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
    globalThis.document.cookie = "aipermission_workspace_3210=workspace-two";
    await assert.rejects(() => apiPost("/api/connector-actions/local-run", body));
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
  globalThis.window = {
    get localStorage() {
      throw new Error("denied");
    },
  };
  globalThis.fetch = async () => {
    fetched = true;
    return response(localActionResponse());
  };
  try {
    const browserModule = await import(`./api.js?retry-storage-denied=${Date.now()}`);
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
  globalThis.window = {
    localStorage: memoryStorage(),
    location: { protocol: "http:", port: "3210" },
    dispatchEvent() {},
  };
  globalThis.document = { cookie: "aipermission_workspace_3210=workspace-no-indexeddb" };
  delete globalThis.indexedDB;
  globalThis.fetch = async () => {
    fetched = true;
    return response(localActionResponse());
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
  const keys = [];
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(options.body).idempotency_key);
    calls += 1;
    if (calls === 1) throw new TypeError("response lost");
    if (calls === 2) return response({ error: "ui session required" }, 401);
    return response(localActionResponse());
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
  globalThis.fetch = async () => {
    calls += 1;
    if (calls === 1) return response(localActionResponse("outcome_unknown"));
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
    globalThis.fetch = async () => response(localActionResponse());
    await apiPost("/api/connector-actions/local-run", body);
  } finally {
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

function response(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async text() {
      return JSON.stringify(body);
    },
  };
}

function rawResponse(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    async text() {
      return body;
    },
  };
}

async function readRetryStoreRecords() {
  const database = await new Promise((resolve, reject) => {
    const request = globalThis.indexedDB.open("aipermission-local-action-retry", 1);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  try {
    return await new Promise((resolve, reject) => {
      const request = database.transaction("entries").objectStore("entries").getAll();
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    });
  } finally {
    database.close();
  }
}

async function deleteRetrySigningKey(scope) {
  const database = await new Promise((resolve, reject) => {
    const request = globalThis.indexedDB.open("aipermission-local-action-retry", 1);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });
  try {
    await new Promise((resolve, reject) => {
      const transaction = database.transaction("keys", "readwrite");
      transaction.objectStore("keys").delete(scope);
      transaction.oncomplete = resolve;
      transaction.onerror = () => reject(transaction.error);
    });
  } finally {
    database.close();
  }
}

function restoreGlobal(name, value) {
  if (value === undefined) delete globalThis[name];
  else globalThis[name] = value;
}

function installFakeBrowserRetryStorage(workspaceID) {
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const originalIndexedDB = globalThis.indexedDB;
  const originalIDBKeyRange = globalThis.IDBKeyRange;
  globalThis.window = {
    localStorage: memoryStorage(),
    location: { protocol: "http:", port: "3210" },
    dispatchEvent() {},
  };
  globalThis.document = { cookie: `aipermission_workspace_3210=${workspaceID}` };
  globalThis.indexedDB = fakeRetryIndexedDB;
  globalThis.IDBKeyRange = IDBKeyRange;
  return () => {
    restoreWindow(originalWindow);
    restoreDocument(originalDocument);
    restoreGlobal("indexedDB", originalIndexedDB);
    restoreGlobal("IDBKeyRange", originalIDBKeyRange);
  };
}

function localActionResponse(status = "completed") {
  return { request_id: 41, status };
}

function restoreWindow(value) {
  if (value === undefined) delete globalThis.window;
  else globalThis.window = value;
}

function restoreDocument(value) {
  if (value === undefined) delete globalThis.document;
  else globalThis.document = value;
}

function memoryStorage() {
  const values = new Map();
  return {
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    setItem(key, value) {
      values.set(key, String(value));
    },
    removeItem(key) {
      values.delete(key);
    },
    values() {
      return values.values();
    },
  };
}
