import assert from "node:assert/strict";
import test from "node:test";
import { IDBFactory, IDBKeyRange } from "fake-indexeddb";

import { APIError } from "../../lib/errors.ts";
import { apiPost } from "../../lib/api.ts";
import {
  completeLocalActionRetry,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  resetLocalActionRetryLedger,
  retireLocalActionRetryAttempt,
} from "../../lib/local-action-retry.ts";

const fakeRetryIndexedDB = new IDBFactory();

test("backup upload retries rotate an expired remote operation identity", async () => {
  const originalFetch = globalThis.fetch;
  const bodies: Record<string, unknown>[] = [];
  globalThis.fetch = async (_url, options) => {
    bodies.push(JSON.parse(String(options?.body)));
    if (bodies.length === 1) throw new TypeError("backup response lost");
    if (bodies.length === 2) {
      return response({ error: "the original backup upload result is no longer available", code: "operation_expired" }, 410);
    }
    return response({ id: 8, provider_file_id: "backup-new" });
  };
  try {
    await assert.rejects(() => apiPost("/api/backup/providers/3/upload", {}), /backup response lost/);
    await assert.rejects(
      () => apiPost("/api/backup/providers/3/upload", {}),
      (error) => {
        assert.ok(error instanceof APIError);
        assert.equal(error.status, 410);
        assert.equal(error.code, "operation_expired");
        return true;
      },
    );
    await apiPost("/api/backup/providers/3/upload", {});
    assert.equal(bodies[0].idempotency_key, bodies[1].idempotency_key);
    assert.notEqual(bodies[1].idempotency_key, bodies[2].idempotency_key);
  } finally {
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
  }
});

test("an expired backup identity immediately releases concurrent attempts for a fresh request", async () => {
  const originalFetch = globalThis.fetch;
  const restoreBrowser = installFakeBrowserRetryStorage("workspace-expired-backup-race");
  const keys: unknown[] = [];
  let releaseExpired!: () => void;
  let releaseSibling!: () => void;
  let signalExpiredStarted!: () => void;
  let signalSiblingStarted!: () => void;
  const expiredGate = new Promise<void>((resolve) => {
    releaseExpired = resolve;
  });
  const siblingGate = new Promise<void>((resolve) => {
    releaseSibling = resolve;
  });
  const expiredStarted = new Promise<void>((resolve) => {
    signalExpiredStarted = resolve;
  });
  const siblingStarted = new Promise<void>((resolve) => {
    signalSiblingStarted = resolve;
  });
  let calls = 0;
  globalThis.fetch = async (_url, options) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    calls += 1;
    if (calls === 1) {
      signalExpiredStarted();
      await expiredGate;
      return response({ error: "backup operation expired", code: "operation_expired" }, 410);
    }
    if (calls === 2) {
      signalSiblingStarted();
      await siblingGate;
      throw new TypeError("sibling response lost");
    }
    return response({ id: 9, provider_file_id: "fresh-backup" });
  };
  try {
    const first = apiPost("/api/backup/providers/3/upload", {}).then(
      () => null,
      (error) => error,
    );
    await expiredStarted;
    const sibling = apiPost("/api/backup/providers/3/upload", {}).then(
      () => null,
      (error) => error,
    );
    await siblingStarted;

    releaseExpired();
    const firstError: unknown = await first;
    assert.ok(firstError instanceof APIError);
    assert.equal(firstError.code, "operation_expired");
    await apiPost("/api/backup/providers/3/upload", {});
    assert.equal(calls, 3);
    assert.equal(keys[0], keys[1]);
    assert.notEqual(keys[1], keys[2]);

    releaseSibling();
    const siblingError: unknown = await sibling;
    assert.ok(siblingError instanceof Error);
    assert.match(siblingError.message, /retry identity changed/i);
  } finally {
    releaseExpired?.();
    releaseSibling?.();
    globalThis.fetch = originalFetch;
    await resetLocalActionRetryLedger();
    restoreBrowser();
  }
});

test("memory retry storage releases concurrent attempts before issuing a fresh key", async () => {
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  Reflect.deleteProperty(globalThis, "window");
  Reflect.deleteProperty(globalThis, "document");
  const body = { path: "/api/backup/providers/3/upload", body: {} };
  try {
    await resetLocalActionRetryLedger();
    const first = await prepareLocalActionRetry(body);
    const sibling = await prepareLocalActionRetry(body);
    assert.equal(first.idempotencyKey, sibling.idempotencyKey);

    assert.equal(await retireLocalActionRetryAttempt(first), true);
    const fresh = await prepareLocalActionRetry(body);
    assert.notEqual(fresh.idempotencyKey, first.idempotencyKey);
    await completeLocalActionRetry(fresh);
    await assert.rejects(() => preserveLocalActionRetryAttempt(sibling), /retry identity changed/i);
  } finally {
    await resetLocalActionRetryLedger();
    restoreGlobal("window", originalWindow);
    restoreGlobal("document", originalDocument);
  }
});

for (const storage of ["memory", "indexeddb"]) {
  test(`${storage} retirement removes sibling attempts from older revisions`, async () => {
    const restoreStorage =
      storage === "indexeddb" ? installFakeBrowserRetryStorage(`workspace-mixed-revision-${storage}`) : installMemoryRetryStorage();
    const body = { path: "/api/backup/providers/3/upload", body: {} };
    try {
      await resetLocalActionRetryLedger();
      const first = await prepareLocalActionRetry(body);
      const sibling = await prepareLocalActionRetry(body);
      assert.equal(first.revision, 1);
      assert.equal(sibling.revision, 1);

      await preserveLocalActionRetryAttempt(first);
      const latest = await prepareLocalActionRetry(body);
      assert.equal(latest.revision, 2);
      assert.equal(latest.idempotencyKey, sibling.idempotencyKey);

      assert.equal(await retireLocalActionRetryAttempt(latest), true);
      const fresh = await prepareLocalActionRetry(body);
      assert.notEqual(fresh.idempotencyKey, latest.idempotencyKey);
      await completeLocalActionRetry(fresh);
      await assert.rejects(() => preserveLocalActionRetryAttempt(sibling), /retry identity changed/i);
    } finally {
      await resetLocalActionRetryLedger();
      restoreStorage();
    }
  });
}

function response(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status });
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
    restoreGlobal("window", originalWindow);
    restoreGlobal("document", originalDocument);
    restoreGlobal("indexedDB", originalIndexedDB);
    restoreGlobal("IDBKeyRange", originalIDBKeyRange);
  };
}

function installMemoryRetryStorage() {
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  Reflect.deleteProperty(globalThis, "window");
  Reflect.deleteProperty(globalThis, "document");
  return () => {
    restoreGlobal("window", originalWindow);
    restoreGlobal("document", originalDocument);
  };
}

function restoreGlobal(name: string, value: unknown) {
  if (value === undefined) Reflect.deleteProperty(globalThis, name);
  else Reflect.set(globalThis, name, value);
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
  };
}
