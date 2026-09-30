import { IDBFactory, IDBKeyRange } from "fake-indexeddb";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, currentWorkspaceBinding } from "../api";
import {
  listLocalActionRetryEntries,
  localActionRetryLedgerChangedEvent,
  localActionRetryObservationFailedEvent,
  prepareLocalActionRetry,
  preserveLocalActionRetryAttempt,
  releaseLocalActionRetryAttempt,
  resetLocalActionRetryLedger,
} from "../local-action-retry";
import * as entries from "../local-action-retry/entries";
import { observeLocalActionRetryResponse } from "../local-action-retry/observations";
import { resetRetryStorage } from "../local-action-retry/storage";
import { connectorActionFixture, connectorApprovalFixture } from "../../test/connector-action-fixtures";
import { scopedUICookieName } from "../ui-cookie";
import type { ConnectorApproval } from "../gateway-contracts/security-contracts";

const workspace = "observed-retry-workspace";
const body = { target_ref: "example:1:1", action_name: "example_action", input: {}, reason: "terminal observation control" };

beforeEach(async () => {
  vi.stubGlobal("indexedDB", new IDBFactory());
  vi.stubGlobal("IDBKeyRange", IDBKeyRange);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=${workspace}; path=/`;
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response("{}", {
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": workspace,
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
  );
  await apiGet("/api/status");
  await resetLocalActionRetryLedger();
});

afterEach(async () => {
  vi.restoreAllMocks();
  await resetLocalActionRetryLedger();
  vi.unstubAllGlobals();
});

for (const kind of ["detail", "list"] as const) {
  for (const status of ["completed", "failed", "canceled", "blocked", "stale", "declined", "error"] as const) {
    it(`settles only the matching pending request from a verified ${kind} ${status} read`, async () => {
      const keys = installGateway(approval({ status }), kind);
      await apiPost("/api/connector-actions/local-run", body);
      expect(await listLocalActionRetryEntries()).toHaveLength(1);
      await apiGet(observationPath(kind));
      expect(await listLocalActionRetryEntries()).toEqual([]);
      await apiPost("/api/connector-actions/local-run", body);
      expect(keys[1]).not.toBe(keys[0]);
    });
  }
}

it.each(["running", "approval_pending", "outcome_unknown"] as const)("does not settle a %s observation", async (status) => {
  installGateway(approval({ status }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toEqual([
    expect.objectContaining({ state: status === "outcome_unknown" ? "outcome_unknown" : "pending" }),
  ]);
});

it.each([{ id: 99 }, { target_ref: "example:2:2" }, { action_name: "different_action" }])(
  "does not consume another request identity %j",
  async (overrides) => {
    installGateway(approval({ status: "completed", ...overrides }), "list");
    await apiPost("/api/connector-actions/local-run", body);
    await apiGet(observationPath("list"));
    expect(await listLocalActionRetryEntries()).toHaveLength(1);
  },
);

it.each(["another-workspace", ""])("ignores observations without the captured workspace binding %s", async (responseWorkspace) => {
  installGateway(approval({ status: "completed" }), "detail", responseWorkspace);
  await apiPost("/api/connector-actions/local-run", body);
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it.each(["another-workspace", ""])(
  "rejects an explicitly bound observation from workspace %j without settling its identity",
  async (responseWorkspace) => {
    installGateway(approval({ status: "completed" }), "detail", responseWorkspace);
    await apiPost("/api/connector-actions/local-run", body);
    await expect(apiGet(observationPath("detail"), { workspaceBinding: workspace })).rejects.toThrow(/workspace binding mismatch/);
    expect(await entries.allEntries({ key: workspace })).toHaveLength(1);
  },
);

it("sends the captured binding only for explicit reads and settles a matching observation", async () => {
  installGateway(approval({ status: "completed" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const read = vi.fn(async (_url: string, _options?: RequestInit) => json(approval({ status: "completed" })));
  vi.stubGlobal("fetch", read);
  await apiGet("/api/status");
  expect(read.mock.calls[0]?.[1]).not.toHaveProperty("headers");
  await apiGet(observationPath("detail"), { workspaceBinding: workspace });
  expect(read).toHaveBeenLastCalledWith(
    expect.stringContaining(observationPath("detail")),
    expect.objectContaining({
      headers: { "X-AIPermission-Workspace": workspace },
    }),
  );
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("does not let a late explicitly bound read replace the current workspace", async () => {
  installGateway(approval({ status: "completed" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const reply = deferred<Response>();
  vi.stubGlobal("fetch", () => reply.promise);
  const reading = apiGet(observationPath("detail"), { workspaceBinding: workspace });
  const nextWorkspace = "next-explicit-workspace";
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response("{}", {
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": nextWorkspace,
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
  );
  await apiGet("/api/status");
  reply.resolve(
    new Response(JSON.stringify(approval({ status: "completed" })), {
      headers: {
        "Content-Type": "application/json",
        "X-AIPermission-Workspace": workspace,
        "X-AIPermission-Workspace-Changed": "true",
      },
    }),
  );
  await reading;
  expect(currentWorkspaceBinding()).toBe(nextWorkspace);
  expect(await entries.allEntries({ key: workspace })).toEqual([]);
});

it("rejects a wrong detail ID and malformed list without changing retry state", async () => {
  installGateway(approval({ id: 99, status: "completed" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  await expect(apiGet(observationPath("detail"))).rejects.toThrow(/observation identity/);
  vi.stubGlobal("fetch", async () => json([{ ...approval({ status: "completed" }), status: "future-status" }]));
  await expect(apiGet(observationPath("list"))).rejects.toThrow(/Invalid connector approvals/);
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it("does not automatically settle an already unknown ledger entry", async () => {
  installGateway(approval({ status: "completed" }), "detail", workspace, "outcome_unknown");
  await apiPost("/api/connector-actions/local-run", body);
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown" })]);
});

it("validates the entire list before settling any matching entry", async () => {
  installGateway(approval({ status: "completed" }), "list");
  await apiPost("/api/connector-actions/local-run", body);
  vi.stubGlobal("fetch", async () => json([approval({ status: "completed" }), { ...approval({ id: 99 }), status: "invalid" }]));
  await expect(apiGet(observationPath("list"))).rejects.toThrow(/Invalid connector approvals/);
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it("ignores unrelated routes without reading local retry storage", async () => {
  const read = vi.spyOn(entries, "allEntries");
  vi.stubGlobal("fetch", async () => json({ status: "completed" }));
  await apiGet("/api/health");
  await apiGet("/api/connector-action-approvals-other");
  expect(read).not.toHaveBeenCalled();
});

it.each(["load", "settle", "promote"])("keeps a successful canonical read usable when local storage cannot %s", async (operation) => {
  const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
  const changed = vi.fn();
  const failed = vi.fn();
  window.addEventListener(localActionRetryLedgerChangedEvent, changed);
  window.addEventListener(localActionRetryObservationFailedEvent, failed);
  try {
    const observed = approval({ status: operation === "promote" ? "outcome_unknown" : "completed" });
    installGateway(observed, "detail");
    await apiPost("/api/connector-actions/local-run", body);
    changed.mockClear();
    const failure =
      operation === "load"
        ? vi.spyOn(entries, "allEntries")
        : operation === "promote"
          ? vi.spyOn(entries, "updateEntryIfMatching")
          : vi.spyOn(entries, "deleteEntryIfMatching");
    failure.mockRejectedValueOnce(new Error("synthetic storage failure with private metadata"));
    expect(await apiGet(observationPath("detail"))).toEqual(observed);
    expect(await listLocalActionRetryEntries()).toHaveLength(1);
    expect(changed).not.toHaveBeenCalled();
    expect(failed).toHaveBeenCalledOnce();
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("could not be persisted"));
    expect(JSON.stringify(warning.mock.calls)).not.toContain("private metadata");
  } finally {
    window.removeEventListener(localActionRetryLedgerChangedEvent, changed);
    window.removeEventListener(localActionRetryObservationFailedEvent, failed);
  }
});

it.each(["completed", "outcome_unknown"] as const)("observes %s safely through the non-browser memory adapter", async (status) => {
  const browserWindow = window;
  vi.stubGlobal("window", undefined);
  try {
    const prepared = await prepareLocalActionRetry(body, { workspaceID: workspace });
    await preserveLocalActionRetryAttempt(prepared, { request_id: 71, ...body });
    await observeLocalActionRetryResponse(observationPath("detail"), approval({ status }), workspace);
    const remaining = await entries.allEntries(prepared.scope);
    expect(remaining).toEqual(status === "completed" ? [] : [expect.objectContaining({ state: "outcome_unknown", request_id: 71 })]);
  } finally {
    await resetRetryStorage();
    vi.stubGlobal("window", browserWindow);
  }
});

it("does not promote a changed revision from a stale unknown snapshot", async () => {
  installGateway(approval({ status: "outcome_unknown" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const original = entries.updateEntryIfMatching;
  const entered = deferred<void>();
  const resume = deferred<void>();
  vi.spyOn(entries, "updateEntryIfMatching").mockImplementationOnce(async (...args) => {
    entered.resolve();
    await resume.promise;
    return original(...args);
  });
  const reading = apiGet(observationPath("detail"));
  await entered.promise;
  await apiPost("/api/connector-actions/local-run", body);
  resume.resolve();
  await reading;
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "pending" })]);
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown" })]);
});

it("does not delete an entry whose revision advanced after the observation snapshot", async () => {
  installGateway(approval({ status: "completed" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const original = entries.deleteEntryIfMatching;
  const entered = deferred<void>();
  const resume = deferred<void>();
  vi.spyOn(entries, "deleteEntryIfMatching").mockImplementationOnce(async (...args) => {
    entered.resolve();
    await resume.promise;
    return original(...args);
  });
  const reading = apiGet(observationPath("detail"));
  await entered.promise;
  await apiPost("/api/connector-actions/local-run", body);
  resume.resolve();
  await reading;
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("retains terminal observations until another active attempt has drained", async () => {
  installGateway(approval({ status: "completed" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const active = await prepareLocalActionRetry({ path: "/api/connector-actions/local-run", body });
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
  await releaseLocalActionRetryAttempt(active);
  await apiGet(observationPath("detail"));
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("keeps a read-promoted unknown sticky across an active pending reply and blocks automatic POST dispatch", async () => {
  const keys = installGateway(approval({ status: "outcome_unknown" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const active = await prepareLocalActionRetry({ path: "/api/connector-actions/local-run", body });
  await apiGet(observationPath("detail"));
  await preserveLocalActionRetryAttempt(active, { request_id: 71, ...body });
  expect(await listLocalActionRetryEntries()).toEqual([
    expect.objectContaining({ state: "outcome_unknown", target_ref: body.target_ref, action_name: body.action_name }),
  ]);
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow(/canceled/);
  expect(keys).toHaveLength(1);
});

it("preserves the original identity while promoting a conflicting unknown error", async () => {
  installGateway(approval(), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response(
        JSON.stringify({
          error: "synthetic uncertainty",
          status: "outcome_unknown",
          request_id: 71,
          target_ref: "example:99:99",
          action_name: "another_action",
        }),
        { status: 409, headers: { "Content-Type": "application/json" } },
      ),
  );
  await expect(apiPost("/api/connector-actions/local-run", body)).rejects.toThrow("synthetic uncertainty");
  expect(await listLocalActionRetryEntries()).toEqual([
    expect.objectContaining({ state: "outcome_unknown", request_id: 71, target_ref: body.target_ref, action_name: body.action_name }),
  ]);
});

it("settles a late read only in its captured workspace, never the newly selected workspace", async () => {
  installGateway(approval({ status: "completed" }), "detail");
  await apiPost("/api/connector-actions/local-run", body);
  const reply = deferred<Response>();
  vi.stubGlobal("fetch", () => reply.promise);
  const reading = apiGet(observationPath("detail"));
  const nextWorkspace = "next-observed-workspace";
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=${nextWorkspace}; path=/`;
  const nextAttempt = await prepareLocalActionRetry({ path: "/api/connector-actions/local-run", body }, { workspaceID: nextWorkspace });
  await preserveLocalActionRetryAttempt(nextAttempt, { request_id: 71, ...body });
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response("{}", {
        headers: {
          "Content-Type": "application/json",
          "X-AIPermission-Workspace": nextWorkspace,
          "X-AIPermission-Workspace-Changed": "true",
        },
      }),
  );
  await apiGet("/api/status");
  reply.resolve(json(approval({ status: "completed" })));
  await reading;
  expect(await entries.allEntries(nextAttempt.scope)).toHaveLength(1);
  expect(await entries.allEntries({ key: workspace })).toEqual([]);
});

it("stores only request identity metadata, never action input or observed outputs", async () => {
  installGateway(approval({ status: "running", input: { secret: "output-canary" }, output: { secret: "output-canary" } }), "detail");
  await apiPost("/api/connector-actions/local-run", { ...body, input: { secret: "input-canary" } });
  await apiGet(observationPath("detail"));
  const persisted = JSON.stringify(await listLocalActionRetryEntries());
  expect(persisted).not.toContain("input-canary");
  expect(persisted).not.toContain("output-canary");
});

function observationPath(kind: "detail" | "list") {
  return kind === "detail" ? "/api/connector-action-approvals/71" : "/api/connector-action-approvals?active=false";
}

function approval(overrides: Partial<ConnectorApproval> = {}) {
  return connectorApprovalFixture({ id: 71, target_ref: body.target_ref, action_name: body.action_name, ...overrides });
}

function installGateway(
  item: ConnectorApproval,
  kind: "detail" | "list",
  responseWorkspace = workspace,
  callStatus: ConnectorApproval["status"] = "running",
) {
  const keys: unknown[] = [];
  vi.stubGlobal("fetch", async (_url: string, options?: RequestInit) => {
    if (options?.method === "POST") {
      keys.push(JSON.parse(String(options.body)).idempotency_key);
      return json(
        connectorActionFixture({
          request_id: 71,
          target_ref: body.target_ref,
          action_name: body.action_name,
          status: callStatus,
        }),
      );
    }
    return json(kind === "detail" ? item : [item], responseWorkspace);
  });
  return keys;
}

function deferred<T>() {
  let resolve!: (_value: T | PromiseLike<T>) => void;
  const promise = new Promise<T>((finish) => {
    resolve = finish;
  });
  return { promise, resolve };
}

function json(value: unknown, responseWorkspace = workspace) {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json", "X-AIPermission-Workspace": responseWorkspace },
  });
}
