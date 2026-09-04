import assert from "node:assert/strict";
import test from "node:test";

import { apiDownload, apiPost } from "./api.js";
import { listLocalActionRetryEntries, resolveLocalActionRetryEntry } from "./local-action-retry.js";

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
    const [entry] = listLocalActionRetryEntries();
    assert.equal(entry.state, "outcome_unknown");
    assert.equal(resolveLocalActionRetryEntry(entry.signature), true);
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
  const keys = [];
  const storage = memoryStorage();
  globalThis.window = { localStorage: storage, location: { protocol: "http:", port: "3210" } };
  globalThis.document = { cookie: "aipermission_workspace_3210=workspace-one" };
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
    assert.equal(
      [...storage.values()].some((value) => value.includes("not-persisted")),
      false,
    );

    const reloadedModule = await import(`./api.js?retry-reload=${Date.now()}`);
    await reloadedModule.apiPost("/api/connector-actions/local-run", body);
    await reloadedModule.apiPost("/api/connector-actions/local-run", body);
    assert.equal(keys[0], keys[1]);
    assert.notEqual(keys[1], keys[2]);
  } finally {
    globalThis.fetch = originalFetch;
    restoreWindow(originalWindow);
    restoreDocument(originalDocument);
  }
});

test("browser retry keys are isolated by persistent workspace identity", async () => {
  const originalFetch = globalThis.fetch;
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const keys = [];
  const storage = memoryStorage();
  globalThis.window = { localStorage: storage, location: { protocol: "http:", port: "3210" } };
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
