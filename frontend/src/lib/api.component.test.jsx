import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { apiDownload, apiGet, apiPost } from "./api";
import {
  localActionReconciliationEvent,
  completeLocalActionRetry,
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  releaseLocalActionRetryAttempt,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "./local-action-retry";
import { scopedUICookieName } from "./ui-cookie";

beforeEach(async () => {
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=browser-retry-test; path=/`;
  await resetLocalActionRetryLedger();
});

afterEach(async () => {
  await resetLocalActionRetryLedger();
  vi.unstubAllGlobals();
});

it("rotates the browser retry identity after an acknowledged action", async () => {
  const keys = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse({ request_id: keys.length, status: "completed" });
    }),
  );
  const body = { target_ref: "fixture:1:1", action_name: "inspect", input: {}, reason: "coverage" };

  await apiPost("/api/connector-actions/local-run", body);
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys).toHaveLength(2);
  expect(keys[0]).not.toBe(keys[1]);
});

it("retires a fresh retry identity after a definitive client rejection", async () => {
  const keys = [];
  let calls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      return calls === 1 ? jsonResponse({ error: "invalid request" }, 400) : jsonResponse({ request_id: 2, status: "completed" });
    }),
  );
  const body = { target_ref: "fixture:2:1", action_name: "mutate", input: {}, reason: "coverage" };

  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toMatchObject({ status: 400 });
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys[0]).not.toBe(keys[1]);
});

it("retains the browser retry identity when an acknowledgement is malformed", async () => {
  const keys = [];
  let calls = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      return calls === 1 ? jsonResponse({ status: "completed" }) : jsonResponse({ request_id: 2, status: "completed" });
    }),
  );
  const body = { target_ref: "fixture:3:1", action_name: "mutate", input: {}, reason: "coverage" };

  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/Invalid connector action response/);
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys[0]).toBe(keys[1]);
});

it("retires a reconciled fresh identity after a definitive client rejection", async () => {
  const keys = [];
  let calls = 0;
  window.addEventListener(localActionReconciliationEvent, (event) => event.detail.resolve(true), { once: true });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      if (calls === 1) return jsonResponse({ request_id: 1, status: "outcome_unknown" });
      if (calls === 2) return jsonResponse({ error: "invalid request" }, 400);
      return jsonResponse({ request_id: 3, status: "completed" });
    }),
  );
  const body = { target_ref: "fixture:4:1", action_name: "mutate", input: {}, reason: "coverage" };

  await apiPost("/api/connector-actions/local-run", body);
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toMatchObject({ status: 400 });
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys[1]).not.toBe(keys[0]);
  expect(keys[2]).not.toBe(keys[1]);
});

it("accepts concurrent unknown outcomes for one retry identity", async () => {
  const body = { target_ref: "fixture:5:1", action_name: "mutate", input: {}, reason: "coverage" };
  const first = await prepareLocalActionRetry(body);
  const second = await prepareLocalActionRetry(body);
  expect(second.idempotencyKey).toBe(first.idempotencyKey);

  await Promise.all([
    markLocalActionRetryOutcome(first, { request_id: 11, assistant_hint: "Inspect state." }),
    markLocalActionRetryOutcome(second, { request_id: 12, assistant_hint: "Inspect state." }),
  ]);

  const [entry] = await listLocalActionRetryEntries();
  expect(entry).toMatchObject({ key: first.idempotencyKey, state: "outcome_unknown" });
});

it("persists an acknowledged unknown outcome for a local action", async () => {
  const keys = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse({ request_id: 41, status: "outcome_unknown", assistant_hint: "Inspect state before retrying." });
    }),
  );
  const body = { target_ref: "fixture:6:1", action_name: "mutate", input: {}, reason: "coverage" };

  await apiPost("/api/connector-actions/local-run", body);
  const [entry] = await listLocalActionRetryEntries();

  expect(entry).toMatchObject({ key: keys[0], state: "outcome_unknown" });
});

it("does not start a download when the native picker is canceled", async () => {
  const abort = new DOMException("Canceled", "AbortError");
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  window.showSaveFilePicker = vi.fn(async () => {
    throw abort;
  });

  await expect(apiDownload("/api/download", "backup:latest.aipdb", { picker: true })).resolves.toEqual({
    saved: false,
    canceled: true,
    method: "picker",
  });
  expect(fetch).not.toHaveBeenCalled();
  delete window.showSaveFilePicker;
});

it("announces an expired UI session while retaining structured API error data", async () => {
  const listener = vi.fn();
  window.addEventListener("aipermission:ui-session-required", listener, { once: true });
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => jsonResponse({ error: "ui session required", code: "session_required" }, 401)),
  );

  await expect(apiGet("/api/private")).rejects.toMatchObject({ status: 401, code: "session_required" });

  expect(listener).toHaveBeenCalledOnce();
});

it("keeps an unresolved retry entry visible until explicit reconciliation", async () => {
  const body = { target_ref: "fixture:7:1", action_name: "mutate", input: {}, reason: "coverage" };
  const prepared = await prepareLocalActionRetry(body);
  await markLocalActionRetryOutcome(prepared, { request_id: 73, assistant_hint: "Inspect state." });

  const [entry] = await listLocalActionRetryEntries();
  expect(entry).toMatchObject({ request_id: 73, assistant_hint: "Inspect state.", state: "outcome_unknown" });

  window.addEventListener(localActionReconciliationEvent, (event) => event.detail.resolve(true), { once: true });
  const replacement = await prepareLocalActionRetry(body);
  expect(replacement.reused).toBe(false);
});

it("removes an explicitly reconciled retry entry", async () => {
  const prepared = await prepareLocalActionRetry({
    target_ref: "fixture:8:1",
    action_name: "mutate",
    input: {},
    reason: "coverage",
  });
  await markLocalActionRetryOutcome(prepared, { request_id: 81 });
  const [entry] = await listLocalActionRetryEntries();

  await expect(resolveLocalActionRetryEntry(entry)).resolves.toBe(true);
  await expect(listLocalActionRetryEntries()).resolves.toEqual([]);
});

it("can release an unfinished attempt without deleting its retry identity", async () => {
  const prepared = await prepareLocalActionRetry({
    target_ref: "fixture:9:1",
    action_name: "mutate",
    input: {},
    reason: "coverage",
  });

  await expect(releaseLocalActionRetryAttempt(prepared)).resolves.toBeUndefined();
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it("fails closed before reserving a retry identity when secure hashing is unavailable", async () => {
  vi.stubGlobal("crypto", {});

  await expect(
    prepareLocalActionRetry({ target_ref: "fixture:10:1", action_name: "mutate", input: {}, reason: "coverage" }),
  ).rejects.toThrow(/secure request hashing is unavailable/i);
});

it("releases a signing reservation when request hashing fails", async () => {
  const originalCrypto = globalThis.crypto;
  vi.stubGlobal("crypto", {
    randomUUID: () => originalCrypto.randomUUID(),
    subtle: {
      generateKey: vi.fn((...args) => originalCrypto.subtle.generateKey.call(originalCrypto.subtle, ...args)),
      sign: vi.fn(async () => {
        throw new Error("hashing failed");
      }),
    },
  });

  await expect(
    prepareLocalActionRetry({ target_ref: "fixture:11:1", action_name: "mutate", input: {}, reason: "coverage" }),
  ).rejects.toThrow("hashing failed");

  // A later attempt can reserve the same scope, proving the failed reservation was released.
  vi.stubGlobal("crypto", originalCrypto);
  const prepared = await prepareLocalActionRetry({
    target_ref: "fixture:11:1",
    action_name: "mutate",
    input: {},
    reason: "coverage",
  });
  await completeLocalActionRetry(prepared);
});

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
