import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { IDBFactory } from "fake-indexeddb";
import { allEntries, getEntry, releaseEntryAttempt, replaceReconciledEntry, reserveEntry } from "./entries.ts";
import { reserveSigningKey } from "./signing.ts";
import { openRetryDatabase, requestPromise, resetRetryStorage, transactionPromise } from "./storage.ts";
import type { PreparedRetry } from "./records.ts";

const scope = { key: "workspace-records" };
const signature = "a".repeat(64);

beforeEach(() => vi.stubGlobal("indexedDB", new IDBFactory()));
afterEach(async () => {
  await resetRetryStorage();
  vi.unstubAllGlobals();
});

async function reserve() {
  const signed = await reserveSigningKey(scope);
  return reserveEntry(scope, signature, signed.id);
}

describe("typed retry entry storage", () => {
  it("shares an existing retry entry without sharing attempt identity", async () => {
    const first = await reserve();
    const second = await reserve();
    expect(first.created).toBe(true);
    expect(second.created).toBe(false);
    expect(second.entry.key).toBe(first.entry.key);
    expect(second.attempt.id).not.toBe(first.attempt.id);
    expect(await allEntries(scope)).toEqual([first.entry]);
    expect(await allEntries({ key: "another-workspace" })).toEqual([]);
  });

  it("rejects corrupt persisted entries before exposing them to callers", async () => {
    const { entry } = await reserve();
    const database = await openRetryDatabase();
    await transactionPromise(database, "entries", "readwrite", (store) => requestPromise(store.put({ ...entry, signature: 42 })));
    await expect(getEntry(scope, signature)).rejects.toThrow("Secure retry storage is unavailable");
    await expect(allEntries(scope)).rejects.toThrow("Secure retry storage is unavailable");
  });

  it("prevents reconciliation from replacing an identity while an attempt is active", async () => {
    const { entry, attempt } = await reserve();
    const prepared: PreparedRetry = { scope, signature, idempotencyKey: entry.key, revision: entry.revision, attemptID: attempt.id, reused: false };
    await expect(replaceReconciledEntry(scope, entry)).rejects.toThrow("retry identity changed");
    await releaseEntryAttempt(prepared);
    const replacement = await replaceReconciledEntry(scope, entry);
    expect(replacement.key).not.toBe(entry.key);
    await expect(replaceReconciledEntry(scope, entry)).rejects.toThrow("retry identity changed");
    expect(await getEntry(scope, signature)).toEqual(replacement);
  });
});
