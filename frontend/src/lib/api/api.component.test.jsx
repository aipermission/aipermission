import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";

import { apiDownload, apiGet, apiPost, apiPostForm, apiPut } from "../api";
import {
  localActionReconciliationEvent,
  completeLocalActionRetry,
  listLocalActionRetryEntries,
  markLocalActionRetryOutcome,
  prepareLocalActionRetry,
  releaseLocalActionRetryAttempt,
  resetLocalActionRetryLedger,
  resolveLocalActionRetryEntry,
} from "../local-action-retry";
import { scopedUICookieName } from "../ui-cookie";

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

it("posts ordinary API requests without adding connector retry metadata", async () => {
  const fetch = vi.fn(async () => jsonResponse({ ok: true }));
  vi.stubGlobal("fetch", fetch);

  await expect(apiPost("/api/settings", { retention_days: 7 })).resolves.toEqual({ ok: true });

  expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual({ retention_days: 7 });
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("does not adopt another workspace after a stale mutation is rejected", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ state: "unlocked" }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": "workspace-a",
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
    )
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ state: "unlocked" }), {
        status: 200,
        headers: { "Content-Type": "application/json", "X-AIPermission-Workspace": "workspace-b" },
      }),
    )
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ error: "workspace changed" }), {
        status: 409,
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": "workspace-b",
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
    )
    .mockResolvedValueOnce(jsonResponse({ ok: true }));
  vi.stubGlobal("fetch", fetch);

  await apiPost("/api/databases/switch", { database_id: "workspace-a" });
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=workspace-b; path=/`;

  await apiGet("/api/status");
  await expect(apiPost("/api/settings", { retention_days: 7 })).rejects.toMatchObject({ status: 409 });
  expect(fetch.mock.calls[2][1].headers["X-AIPermission-Workspace"]).toBe("workspace-a");

  await apiPost("/api/settings", { retention_days: 8 });
  expect(fetch.mock.calls[3][1].headers["X-AIPermission-Workspace"]).toBe("workspace-a");

  // Leave the module-scoped tab binding in the fixture state for later tests.
  fetch.mockResolvedValueOnce(
    new Response(JSON.stringify({ state: "unlocked" }), {
      status: 200,
      headers: {
        "Content-Type": "application/json",
        "X-AIPermission-Workspace": "browser-retry-test",
        "X-AIPermission-Workspace-Changed": "true",
      },
    }),
  );
  await apiPost("/api/databases/switch", { database_id: "browser-retry-test" });
});

it("binds retry storage and mutation headers to the same tab workspace", async () => {
  const body = { target_ref: "fixture:workspace:1", action_name: "mutate", input: {}, reason: "coverage" };
  const protectedEntry = await prepareLocalActionRetry(body, { workspaceID: "workspace-b" });
  await markLocalActionRetryOutcome(protectedEntry, { request_id: 91, assistant_hint: "Inspect workspace B." });

  const fetch = vi
    .fn()
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ state: "unlocked" }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": "workspace-a",
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
    )
    .mockResolvedValueOnce(jsonResponse(actionResponse(body)))
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ state: "unlocked" }), {
        status: 200,
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": "browser-retry-test",
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
    );
  vi.stubGlobal("fetch", fetch);
  const reconciliation = vi.fn((event) => event.detail.resolve(false));
  window.addEventListener(localActionReconciliationEvent, reconciliation, { once: true });

  await apiPost("/api/databases/switch", { database_id: "workspace-a" });
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=workspace-b; path=/`;
  await apiPost("/api/connector-actions/local-run", body);

  expect(fetch.mock.calls[1][1].headers["X-AIPermission-Workspace"]).toBe("workspace-a");
  expect(reconciliation).not.toHaveBeenCalled();
  window.removeEventListener(localActionReconciliationEvent, reconciliation);
  await expect(listLocalActionRetryEntries()).resolves.toEqual([
    expect.objectContaining({ key: protectedEntry.idempotencyKey, state: "outcome_unknown" }),
  ]);

  document.cookie = `${scopedUICookieName("aipermission_workspace")}=browser-retry-test; path=/`;
  await apiPost("/api/databases/switch", { database_id: "browser-retry-test" });
});

it("rotates the browser retry identity after an acknowledged action", async () => {
  const keys = [];
  const body = { target_ref: "fixture:1:1", action_name: "inspect", input: {}, reason: "coverage" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse(actionResponse(body, { request_id: keys.length }));
    }),
  );

  await apiPost("/api/connector-actions/local-run", body);
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys).toHaveLength(2);
  expect(keys[0]).not.toBe(keys[1]);
});

it("retires a fresh retry identity after a definitive client rejection", async () => {
  const keys = [];
  let calls = 0;
  const body = { target_ref: "fixture:2:1", action_name: "mutate", input: {}, reason: "coverage" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      return calls === 1 ? jsonResponse({ error: "invalid request" }, 400) : jsonResponse(actionResponse(body, { request_id: 2 }));
    }),
  );

  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toMatchObject({ status: 400 });
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys[0]).not.toBe(keys[1]);
});

it("retains the browser retry identity when an acknowledgement is malformed", async () => {
  const keys = [];
  let calls = 0;
  const body = { target_ref: "fixture:3:1", action_name: "mutate", input: {}, reason: "coverage" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      return calls === 1 ? jsonResponse({ status: "completed" }) : jsonResponse(actionResponse(body, { request_id: 2 }));
    }),
  );

  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/Invalid connector action response/);
  await apiPost("/api/connector-actions/local-run", body);

  expect(keys[0]).toBe(keys[1]);
});

it("retains one retry identity until the action response matches the dispatched request", async () => {
  const keys = [];
  let calls = 0;
  const body = { target_ref: "fixture:identity:1", action_name: "mutate", input: {}, reason: "coverage" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      if (calls === 1) return jsonResponse(actionResponse(body, { target_ref: "fixture:other:1" }));
      if (calls === 2) return jsonResponse(actionResponse(body, { action_name: "inspect" }));
      if (calls === 3) {
        const response = actionResponse(body);
        delete response.retry_policy;
        return jsonResponse(response);
      }
      return jsonResponse(actionResponse(body));
    }),
  );

  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/Invalid connector action response/);
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/Invalid connector action response/);
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/Invalid connector action response/);
  await apiPost("/api/connector-actions/local-run", body);

  expect(new Set(keys).size).toBe(1);
});

it("rotates the browser retry identity after an acknowledged bulk command", async () => {
  const keys = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse({
        parallelism: 2,
        items: [{ request_id: keys.length, target_id: 4, target_name: "host", status: "running" }],
      });
    }),
  );
  const body = { target_ids: [4], command: "hostname", reason: "coverage", confirmation: "RUN ON 1 TARGETS" };

  await apiPost("/api/console/bulk-exec", body);
  await apiPost("/api/console/bulk-exec", body);

  expect(keys).toHaveLength(2);
  expect(keys[0]).not.toBe(keys[1]);
});

it("retains a bulk retry identity when the gateway acknowledgement is malformed", async () => {
  const keys = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse({ parallelism: 2, items: [] });
    }),
  );
  const body = { target_ids: [4], command: "hostname", reason: "coverage", confirmation: "RUN ON 1 TARGETS" };

  await expect(apiPost("/api/console/bulk-exec", body)).rejects.toThrow(/Invalid bulk command response/);
  await expect(apiPost("/api/console/bulk-exec", body)).rejects.toThrow(/Invalid bulk command response/);

  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
});

it("rotates the browser retry identity after an acknowledged backup upload", async () => {
  const keys = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse({ id: keys.length, provider_file_id: `backup-${keys.length}` });
    }),
  );
  const body = { database_id: "fixture", reason: "coverage" };

  await apiPost("/api/backup/providers/7/upload", body);
  await apiPost("/api/backup/providers/7/upload", body);

  expect(keys).toHaveLength(2);
  expect(keys[0]).not.toBe(keys[1]);
});

it("retains a backup retry identity when the gateway acknowledgement is malformed", async () => {
  const keys = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse({ id: 1, provider_file_id: "" });
    }),
  );
  const body = { database_id: "fixture", reason: "coverage" };

  await expect(apiPost("/api/backup/providers/7/upload", body)).rejects.toThrow(/Invalid backup upload response/);
  await expect(apiPost("/api/backup/providers/7/upload", body)).rejects.toThrow(/Invalid backup upload response/);

  expect(keys).toHaveLength(2);
  expect(keys[0]).toBe(keys[1]);
});

it("preserves a caller-provided idempotency key without opening a browser retry entry", async () => {
  const body = {
    target_ref: "fixture:provided:1",
    action_name: "inspect",
    input: {},
    reason: "coverage",
    idempotency_key: "caller-owned-key",
  };
  const fetch = vi.fn(async (_url, options) =>
    jsonResponse(actionResponse(body, { request_id: 17, echoed: JSON.parse(options.body).idempotency_key })),
  );
  vi.stubGlobal("fetch", fetch);

  await expect(apiPost("/api/connector-actions/local-run", body)).resolves.toMatchObject({
    request_id: 17,
    status: "completed",
    echoed: "caller-owned-key",
  });

  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("retires a reconciled fresh identity after a definitive client rejection", async () => {
  const keys = [];
  let calls = 0;
  const body = { target_ref: "fixture:4:1", action_name: "mutate", input: {}, reason: "coverage" };
  window.addEventListener(localActionReconciliationEvent, (event) => event.detail.resolve(true), { once: true });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      calls += 1;
      if (calls === 1) return jsonResponse(actionResponse(body, { request_id: 1, status: "outcome_unknown" }));
      if (calls === 2) return jsonResponse({ error: "invalid request" }, 400);
      return jsonResponse(actionResponse(body, { request_id: 3 }));
    }),
  );

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
  const body = { target_ref: "fixture:6:1", action_name: "mutate", input: {}, reason: "coverage" };
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url, options) => {
      keys.push(JSON.parse(options.body).idempotency_key);
      return jsonResponse(
        actionResponse(body, { request_id: 41, status: "outcome_unknown", assistant_hint: "Inspect state before retrying." }),
      );
    }),
  );

  await apiPost("/api/connector-actions/local-run", body);
  const [entry] = await listLocalActionRetryEntries();

  expect(entry).toMatchObject({ key: keys[0], state: "outcome_unknown" });
});

function actionResponse(body, overrides = {}) {
  return {
    status: "completed",
    request_id: 1,
    target_ref: body.target_ref,
    connector_kind: "test",
    action_name: body.action_name,
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    ...overrides,
  };
}

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

it("requires JSON from multipart endpoints by default", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("<html>gateway fallback</html>", { status: 200 })),
  );

  await expect(apiPostForm("/api/restore", new FormData())).rejects.toThrow(/HTML instead of JSON/);
});

it("rejects plain text and empty success bodies on ordinary JSON endpoints", async () => {
  const fetch = vi
    .fn()
    .mockResolvedValueOnce(new Response("upstream accepted", { status: 200 }))
    .mockResolvedValueOnce(new Response(null, { status: 204 }));
  vi.stubGlobal("fetch", fetch);

  await expect(apiGet("/api/metadata")).rejects.toThrow(/Invalid JSON response/);
  await expect(apiPut("/api/metadata", {})).rejects.toThrow(/Empty JSON response/);
  expect(fetch).toHaveBeenCalledTimes(2);
});

it("retains HTTP failure status when its response is not JSON", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => new Response("upstream unavailable", { status: 502 })),
  );

  await expect(apiGet("/api/metadata")).rejects.toMatchObject({ status: 502, message: "Invalid JSON response from gateway." });
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
