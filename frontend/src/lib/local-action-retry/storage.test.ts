import assert from "node:assert/strict";
import test from "node:test";
import { IDBFactory } from "fake-indexeddb";
import { requestPromise, transactionPromise, withMemoryTransaction } from "./storage.ts";

async function testDatabase() {
  const request = new IDBFactory().open("transaction-test", 1);
  request.onupgradeneeded = () => {
    request.result.createObjectStore("entries");
  };
  return requestPromise(request);
}

test("retry storage commits successful operations and preserves operation errors", async () => {
  const database = await testDatabase();
  try {
    await transactionPromise(database, "entries", "readwrite", (store) => requestPromise(store.put({ revision: 1 }, "one")));
    const result: unknown = await transactionPromise(database, "entries", "readonly", (store) => requestPromise(store.get("one")));
    assert.deepEqual(result, { revision: 1 });
    const error = new Error("operation refused");
    await assert.rejects(
      transactionPromise(database, "entries", "readwrite", () => {
        throw error;
      }),
      (value) => value === error,
    );
  } finally {
    database.close();
  }
});

test("an IndexedDB transaction cannot acknowledge an unfinished async operation", async () => {
  const database = await testDatabase();
  try {
    await assert.rejects(
      transactionPromise(database, "entries", "readonly", () => new Promise<void>(() => {})),
      /Secure retry storage is unavailable/,
    );
  } finally {
    database.close();
  }
});

test("memory transactions remain serialized and recover after an operation failure", async () => {
  let finish: (() => void) | undefined;
  const order: string[] = [];
  const first = withMemoryTransaction(
    () =>
      new Promise<void>((resolve) => {
        order.push("first");
        finish = resolve;
      }),
  );
  const second = withMemoryTransaction(() => {
    order.push("second");
    throw new Error("refused");
  });
  const failed = assert.rejects(second, /refused/);
  const third = withMemoryTransaction(() => {
    order.push("third");
    return 42;
  });
  await Promise.resolve();
  assert.deepEqual(order, ["first"]);
  if (!finish) throw new Error("First memory operation did not start");
  finish();
  await first;
  await failed;
  assert.equal(await third, 42);
  assert.deepEqual(order, ["first", "second", "third"]);
});
