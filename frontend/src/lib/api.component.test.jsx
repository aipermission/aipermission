import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { apiPost } from "./api";
import {
  localActionReconciliationEvent,
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  resetLocalActionRetryLedger,
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

function jsonResponse(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}
