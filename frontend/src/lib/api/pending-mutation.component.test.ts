import { IDBFactory, IDBKeyRange, IDBVersionChangeEvent } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../api";
import { listLocalActionRetryEntries, resetLocalActionRetryLedger, resolveLocalActionRetryEntry } from "../local-action-retry";
import { openRetryDatabase } from "../local-action-retry/storage";
import { scopedUICookieName } from "../ui-cookie";

const body = { target_ref: "fixture:1:1", action_name: "mutate", input: { value: "synthetic" }, reason: "pending identity control" };

beforeEach(async () => {
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=pending-retry-test; path=/`;
  await resetLocalActionRetryLedger();
});

afterEach(async () => {
  await resetLocalActionRetryLedger();
  vi.unstubAllGlobals();
});

for (const status of ["running", "approval_pending"] as const) {
  it(`retains a lost-reply identity through ${status} replays and browser storage reconnect`, async () => {
    const keys: string[] = [];
    const dispatched = new Set<string>();
    vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
      const request = JSON.parse(String(options.body));
      const key = String(request.idempotency_key);
      keys.push(key);
      dispatched.add(key);
      if (keys.length === 1) throw new TypeError("response lost after dispatch");
      return actionResponse(status);
    });

    await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow("response lost after dispatch");
    await apiPost("/api/connector-actions/local-run", body);
    expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "pending", request_id: 71 })]);
    // Close the browser storage connection, retaining its persisted records.
    const database = await openRetryDatabase();
    database.onversionchange?.(new IDBVersionChangeEvent("versionchange", { oldVersion: database.version }));
    await apiPost("/api/connector-actions/local-run", { ...body, input: { value: "synthetic" } });

    expect(keys).toHaveLength(3);
    expect(new Set(keys).size).toBe(1);
    expect(dispatched.size).toBe(1);
  });
}

for (const status of ["completed", "failed", "canceled", "blocked", "stale", "declined", "error"] as const) {
  it(`releases the pending identity only after a definitive ${status} replay`, async () => {
    const keys: unknown[] = [];
    vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
      keys.push(JSON.parse(String(options.body)).idempotency_key);
      return actionResponse(keys.length === 1 ? "running" : status);
    });
    await apiPost("/api/connector-actions/local-run", body);
    await apiPost("/api/connector-actions/local-run", body);
    expect(await listLocalActionRetryEntries()).toEqual([]);
    await apiPost("/api/connector-actions/local-run", body);
    expect(keys[0]).toBe(keys[1]);
    expect(keys[2]).not.toBe(keys[1]);
  });
}

it("keeps an unknown outcome protected after a pending replay until explicit reconciliation", async () => {
  const keys: unknown[] = [];
  vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
    keys.push(JSON.parse(String(options.body)).idempotency_key);
    return actionResponse(keys.length === 1 ? "running" : "outcome_unknown");
  });
  await apiPost("/api/connector-actions/local-run", body);
  await apiPost("/api/connector-actions/local-run", body);
  const [entry] = await listLocalActionRetryEntries();
  expect(entry).toMatchObject({ state: "outcome_unknown", request_id: 71 });
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/new external attempt was canceled/i);
  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
  await resolveLocalActionRetryEntry(entry);
});

it.each(["indexeddb", "memory"])("%s does not release the pending identity for another terminal request ID", async (storage) => {
  if (storage === "memory") useMemoryStorage();
  const keys: unknown[] = [];
  vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
    keys.push(JSON.parse(String(options.body)).idempotency_key);
    return actionResponse(keys.length === 1 ? "running" : "completed", keys.length === 1 ? 71 : 99);
  });
  await apiPost("/api/connector-actions/local-run", body);
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/retry identity changed/i);
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "pending", request_id: 71 })]);
  expect(keys[0]).toBe(keys[1]);
});

function actionResponse(status: string, requestID = 71) {
  return new Response(
    JSON.stringify({
      status,
      request_id: requestID,
      target_ref: body.target_ref,
      connector_kind: "fixture",
      action_name: body.action_name,
      retry_policy: { class: "non_idempotent", guidance: "Reconcile the original request." },
    }),
    { status: 200, headers: { "Content-Type": "application/json" } },
  );
}

for (const storage of ["indexeddb", "memory"] as const) {
  for (const order of ["pending-first", "unknown-first"] as const) {
    it(`${storage} keeps outcome_unknown sticky when overlapping replies arrive ${order}`, async () => {
      if (storage === "memory") useMemoryStorage();
      const calls = gatedReplies();
      const first = apiPost("/api/connector-actions/local-run", body);
      await vi.waitFor(() => expect(calls.keys).toHaveLength(1));
      const second = apiPost("/api/connector-actions/local-run", body);
      await vi.waitFor(() => expect(calls.keys).toHaveLength(2));
      calls.replies[0].resolve(order === "pending-first" ? actionResponse("running") : actionResponse("outcome_unknown"));
      await first;
      calls.replies[1].resolve(order === "pending-first" ? actionResponse("outcome_unknown") : actionResponse("approval_pending"));
      await second;
      expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown", request_id: 71 })]);
      await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/new external attempt was canceled/i);
      expect(calls.keys).toHaveLength(2);
      expect(calls.keys[0]).toBe(calls.keys[1]);
    });
  }

  for (const order of ["pending-first", "terminal-first"] as const) {
    it(`${storage} retains terminal disposition until overlapping replies drain ${order}`, async () => {
      if (storage === "memory") useMemoryStorage();
      const calls = gatedReplies();
      const first = apiPost("/api/connector-actions/local-run", body);
      await vi.waitFor(() => expect(calls.keys).toHaveLength(1));
      const second = apiPost("/api/connector-actions/local-run", body);
      await vi.waitFor(() => expect(calls.keys).toHaveLength(2));
      calls.replies[0].resolve(order === "pending-first" ? actionResponse("running") : actionResponse("completed"));
      await first;
      if (order === "terminal-first") {
        await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/retry identity changed/i);
        expect(calls.keys).toHaveLength(2);
      }
      calls.replies[1].resolve(order === "pending-first" ? actionResponse("completed") : actionResponse("running"));
      await second;
      expect(await listLocalActionRetryEntries()).toEqual([]);
      const next = apiPost("/api/connector-actions/local-run", body);
      await vi.waitFor(() => expect(calls.keys).toHaveLength(3));
      calls.replies[2].resolve(actionResponse("completed"));
      await next;
      expect(calls.keys[0]).toBe(calls.keys[1]);
      expect(calls.keys[2]).not.toBe(calls.keys[1]);
    });
  }

  it(`${storage} merges an acknowledged request ID after an overlapping transport loss`, async () => {
    if (storage === "memory") useMemoryStorage();
    const calls = gatedReplies();
    const first = apiPost("/api/connector-actions/local-run", body).catch((error: unknown) => error);
    await vi.waitFor(() => expect(calls.keys).toHaveLength(1));
    const second = apiPost("/api/connector-actions/local-run", body);
    await vi.waitFor(() => expect(calls.keys).toHaveLength(2));
    calls.replies[0].reject(new TypeError("response lost"));
    expect(await first).toBeInstanceOf(TypeError);
    calls.replies[1].resolve(actionResponse("running"));
    await second;
    expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "pending", request_id: 71 })]);
    expect(calls.keys[0]).toBe(calls.keys[1]);
  });

  it(`${storage} drains a terminal tombstone even when the last overlapping reply is lost`, async () => {
    if (storage === "memory") useMemoryStorage();
    const calls = gatedReplies();
    const first = apiPost("/api/connector-actions/local-run", body);
    await vi.waitFor(() => expect(calls.keys).toHaveLength(1));
    const second = apiPost("/api/connector-actions/local-run", body).catch((error: unknown) => error);
    await vi.waitFor(() => expect(calls.keys).toHaveLength(2));
    calls.replies[0].resolve(actionResponse("completed"));
    await first;
    calls.replies[1].reject(new TypeError("late response lost"));
    expect(await second).toBeInstanceOf(TypeError);
    expect(await listLocalActionRetryEntries()).toEqual([]);
  });
}

function gatedReplies() {
  const keys: unknown[] = [];
  const replies: { resolve: (_response: Response) => void; reject: (_error: Error) => void }[] = [];
  vi.stubGlobal("fetch", (_url: string, options: RequestInit) => {
    keys.push(JSON.parse(String(options.body)).idempotency_key);
    return new Promise<Response>((resolve, reject) => replies.push({ resolve, reject }));
  });
  return { keys, replies };
}

function useMemoryStorage() {
  vi.stubGlobal("window", undefined);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=pending-retry-test; path=/`;
}
