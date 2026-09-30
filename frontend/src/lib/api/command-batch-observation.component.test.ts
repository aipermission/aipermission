import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, currentWorkspaceBinding } from "../api";
import { listLocalActionRetryEntries } from "../local-action-retry";
import * as entries from "../local-action-retry/entries";
import { resetRetryStorage } from "../local-action-retry/storage";
import { mutationTestWorkspace as workspace, setupMutationRetryStorage } from "../../test/connector-mutation-test-state";
import { connectorApprovalFixture } from "../../test/connector-action-fixtures";
import type { ConsoleCommandStatus } from "../gateway-contracts/console-command-contract";

const path = "/api/console/bulk-exec";
const body = { target_ids: [4, 5], command: "private-command-canary", reason: "private-reason-canary", confirmation: "RUN ON 2 TARGETS" };
const batch = {
  parallelism: 2,
  items: [
    { request_id: 71, target_id: 4, target_name: "first", status: "running" },
    { request_id: 72, target_id: 5, target_name: "second", status: "running" },
  ],
};
setupMutationRetryStorage();
beforeEach(async () => {
  vi.stubGlobal("fetch", async () => json({}, workspace, true));
  await apiGet("/api/status");
});

function gateway(payload: unknown = batch) {
  const keys: string[] = [];
  const details = new Map<number, unknown>();
  const fetch = vi.fn(async (url: string, options?: RequestInit) => {
    if (options?.method === "POST") {
      keys.push(JSON.parse(String(options.body)).idempotency_key);
      return json(payload);
    }
    if (url.endsWith("/api/status")) return json({});
    const id = Number(url.split("/").at(-1));
    return json(details.get(id) || detail(id));
  });
  vi.stubGlobal("fetch", fetch);
  return { keys, details, fetch };
}
const observe = (id: number) => apiGet(`/api/console/command-requests/${id}`, { workspaceBinding: workspace });
function detail(id: number, status: ConsoleCommandStatus = "completed", runtime_id = id === 71 ? 4 : 5) {
  return { id, runtime_id, status, stdout: "private-output-canary" };
}
function json(value: unknown, binding = workspace, changed = false) {
  return new Response(JSON.stringify(value), {
    headers: {
      "Content-Type": "application/json",
      "X-AIPermission-Workspace": binding,
      ...(changed ? { "X-AIPermission-Workspace-Changed": "true" } : {}),
    },
  });
}

it("retains accepted batch identities until every exact command has a definitive outcome", async () => {
  const { keys } = gateway();
  await apiPost(path, body);
  await observe(71);
  expect(await listLocalActionRetryEntries()).toEqual([
    expect.objectContaining({
      request_kind: "console_batch",
      batch_requests: [
        { request_id: 71, target_id: 4, status: "completed", observed: true },
        { request_id: 72, target_id: 5, status: "running" },
      ],
    }),
  ]);
  await apiPost(path, body);
  expect(keys[1]).toBe(keys[0]);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({
    batch_requests: [
      { request_id: 71, target_id: 4, status: "completed", observed: true },
      { request_id: 72, target_id: 5, status: "running" },
    ],
  });
  await observe(72);
  expect(await listLocalActionRetryEntries()).toEqual([]);
  await apiPost(path, body);
  expect(keys[2]).not.toBe(keys[0]);
});

it("does not retire a terminal acknowledgement without separate command observations", async () => {
  const terminal = { ...batch, items: batch.items.map((item) => ({ ...item, status: "completed", observed: true })) };
  const { keys, details } = gateway(terminal);
  await apiPost(path, body);
  await apiPost(path, body);
  expect(keys[1]).toBe(keys[0]);
  const persisted = (await listLocalActionRetryEntries())[0];
  expect(persisted).toEqual(
    expect.objectContaining({
      batch_requests: batch.items.map(({ request_id, target_id }) => ({ request_id, target_id, status: "completed" })),
    }),
  );
  details.set(71, detail(71, "running"));
  await observe(71);
  await observe(72);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({
    batch_requests: [
      { status: "running", observed: true },
      { status: "completed", observed: true },
    ],
  });
  details.set(71, detail(71));
  await observe(71);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it.each(["outcome_unknown", "untracked"] as const)("keeps verified terminal proof after a late %s acknowledgement", async (status) => {
  gateway();
  await apiPost(path, body);
  await observe(71);
  gateway({ ...batch, items: [{ ...batch.items[0], status }, batch.items[1]] });
  await apiPost(path, body);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({
    state: "pending",
    batch_requests: [{ status: "completed", observed: true }, { status: "running" }],
  });
  await observe(72);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("does not promote observed running statuses into terminal proof through a later acknowledgement", async () => {
  const fixture = gateway();
  fixture.details.set(71, detail(71, "running"));
  fixture.details.set(72, detail(72, "running"));
  await apiPost(path, body);
  await Promise.all([observe(71), observe(72)]);
  const terminal = gateway({ ...batch, items: batch.items.map((item) => ({ ...item, status: "completed" })) });
  await apiPost(path, body);
  expect((await listLocalActionRetryEntries())[0]).toEqual(
    expect.objectContaining({
      batch_requests: batch.items.map(({ request_id, target_id }) => ({ request_id, target_id, status: "completed" })),
    }),
  );
  terminal.details.set(71, detail(71, "running"));
  await Promise.all([observe(71), observe(72)]);
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
  terminal.details.set(71, detail(71));
  await observe(71);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it.each(["", "foreign-workspace"])(
  "rejects a bulk acknowledgement with workspace %j without rebinding or importing foreign IDs",
  async (binding) => {
    vi.stubGlobal("fetch", async () => json(batch, binding, true));
    await expect(apiPost(path, body)).rejects.toThrow(/workspace binding mismatch/);
    expect(currentWorkspaceBinding()).toBe(workspace);
    expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ request_kind: "console_batch", state: "pending" })]);
    expect((await listLocalActionRetryEntries())[0]).not.toHaveProperty("batch_requests");
  },
);

it("rejects mixed connector and batch schemas before storing colliding approval identity", async () => {
  gateway({ ...batch, request_id: 71, target_ref: "fixture:4:5", action_name: "mutate" });
  await expect(apiPost(path, body)).rejects.toThrow(/Invalid bulk command response/);
  const entry = (await listLocalActionRetryEntries())[0];
  expect(entry).toMatchObject({ request_kind: "console_batch" });
  for (const field of ["request_id", "target_ref", "action_name", "batch_requests"]) expect(entry).not.toHaveProperty(field);
  vi.stubGlobal("fetch", async () =>
    json(connectorApprovalFixture({ id: 71, target_ref: "fixture:4:5", action_name: "mutate", status: "completed" })),
  );
  await apiGet("/api/connector-action-approvals/71", { workspaceBinding: workspace });
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it.each(["outcome_unknown", "untracked"] as const)(
  "retains a batch with an observed %s result for explicit reconciliation",
  async (status) => {
    const { details } = gateway();
    await apiPost(path, body);
    details.set(71, detail(71, status));
    await observe(71);
    await observe(72);
    details.set(71, detail(71));
    await observe(71);
    expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown" })]);
  },
);

it.each(["outcome_unknown", "untracked"] as const)("protects an accepted batch that already includes %s", async (status) => {
  gateway({ ...batch, items: batch.items.map((item) => ({ ...item, status })) });
  await apiPost(path, body);
  await observe(71);
  await observe(72);
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown" })]);
});

it.each([{ id: 99 }, { runtime_id: 99 }, { status: "future" }, { runtime_id: "4" }])(
  "does not settle a malformed or foreign detail %j",
  async (overrides) => {
    const { details } = gateway();
    await apiPost(path, body);
    details.set(71, { ...detail(71), ...overrides });
    await observe(71).catch(() => {});
    expect((await listLocalActionRetryEntries())[0]).toMatchObject({
      batch_requests: batch.items.map(({ request_id, target_id, status }) => ({ request_id, target_id, status })),
    });
  },
);

it.each(["", "other-workspace"])("rejects a command observation without exact workspace %j", async (binding) => {
  gateway();
  await apiPost(path, body);
  vi.stubGlobal("fetch", async () => json(detail(71), binding));
  await expect(observe(71)).rejects.toThrow(/workspace binding mismatch/);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({ batch_requests: [{ status: "running" }, { status: "running" }] });
});

it("cannot settle a command batch through a colliding connector request ID", async () => {
  gateway();
  await apiPost(path, body);
  vi.stubGlobal("fetch", async () => json(connectorApprovalFixture({ id: 71, status: "completed" })));
  await apiGet("/api/connector-action-approvals/71", { workspaceBinding: workspace });
  expect(await listLocalActionRetryEntries()).toHaveLength(1);
});

it.each([
  {},
  { ...batch, items: [] },
  { ...batch, items: [batch.items[0], batch.items[0]] },
  { ...batch, items: batch.items.map((item) => ({ ...item, target_id: 4 })) },
  { ...batch, items: batch.items.map((item) => ({ ...item, status: "future" })) },
  { ...batch, items: batch.items.map((item) => ({ ...item, target_id: item.target_id + 10 })) },
])("retains one retry key after invalid acceptance %j", async (payload) => {
  const { keys } = gateway(payload);
  await expect(apiPost(path, body)).rejects.toThrow(/Invalid bulk command response/);
  await expect(apiPost(path, body)).rejects.toThrow(/Invalid bulk command response/);
  expect(keys[1]).toBe(keys[0]);
});

it("never stores commands, reasons, target labels or outputs in a batch identity", async () => {
  gateway();
  await apiPost(path, body);
  await observe(71);
  const persisted = JSON.stringify(await listLocalActionRetryEntries());
  for (const canary of [body.command, body.reason, "private-output-canary", "target_name"]) expect(persisted).not.toContain(canary);
});

it("merges immutable command proof across an overlapping replay of the same batch identity", async () => {
  gateway({ ...batch, items: [batch.items[0]] });
  const oneTarget = { ...body, target_ids: [4], confirmation: "RUN ON 1 TARGETS" };
  await apiPost(path, oneTarget);
  let resume!: () => void;
  const waiting = new Promise<void>((resolve) => {
    resume = resolve;
  });
  const original = entries.updateEntryIfMatching;
  let entered = false;
  const update = vi.spyOn(entries, "updateEntryIfMatching").mockImplementationOnce(async (...args) => {
    entered = true;
    await waiting;
    return original(...args);
  });
  const reading = observe(71);
  await vi.waitFor(() => expect(entered).toBe(true));
  await apiPost(path, oneTarget);
  resume();
  await reading;
  expect(await listLocalActionRetryEntries()).toEqual([]);
  update.mockRestore();
  await observe(71);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("retains terminal evidence while an overlapping accepted reply is still in flight", async () => {
  const accepted = { ...batch, items: [batch.items[0]] };
  const oneTarget = { ...body, target_ids: [4], confirmation: "RUN ON 1 TARGETS" };
  gateway(accepted);
  await apiPost(path, oneTarget);
  let finish!: (_response: Response) => void;
  const dispatched = vi.fn(async (_url: string, options?: RequestInit) =>
    options?.method === "POST"
      ? new Promise<Response>((resolve) => {
          finish = resolve;
        })
      : json(detail(71)),
  );
  vi.stubGlobal("fetch", dispatched);
  const replay = apiPost(path, oneTarget);
  await vi.waitFor(() => expect(finish).toBeTypeOf("function"));
  await observe(71);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({ state: "pending", batch_requests: [{ status: "completed" }] });
  finish(json(accepted));
  await replay;
  expect(await listLocalActionRetryEntries()).toEqual([]);
  await observe(71);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("merges concurrent terminal observations instead of stranding a finished batch", async () => {
  gateway();
  await apiPost(path, body);
  await Promise.all([observe(71), observe(72)]);
  expect(await listLocalActionRetryEntries()).toEqual([]);
});

it("does not let an overlapping terminal observation downgrade unknown batch uncertainty", async () => {
  const fixture = gateway({ ...batch, items: [batch.items[0]] });
  const oneTarget = { ...body, target_ids: [4], confirmation: "RUN ON 1 TARGETS" };
  await apiPost(path, oneTarget);
  let resume!: () => void;
  const waiting = new Promise<void>((resolve) => {
    resume = resolve;
  });
  const original = entries.updateEntryIfMatching;
  let entered = false;
  const update = vi.spyOn(entries, "updateEntryIfMatching").mockImplementationOnce(async (...args) => {
    entered = true;
    await waiting;
    return original(...args);
  });
  const reading = observe(71);
  await vi.waitFor(() => expect(entered).toBe(true));
  vi.stubGlobal(
    "fetch",
    async () =>
      new Response(JSON.stringify({ status: "outcome_unknown", error: "synthetic uncertainty" }), {
        status: 409,
        headers: { "Content-Type": "application/json", "X-AIPermission-Workspace": workspace },
      }),
  );
  await expect(apiPost(path, oneTarget)).rejects.toThrow("synthetic uncertainty");
  resume();
  await reading;
  update.mockRestore();
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ state: "outcome_unknown" })]);
  expect(fixture.keys).toHaveLength(1);
});

it("rejects replayed batch IDs that no longer match the originally accepted identity", async () => {
  const { keys } = gateway();
  await apiPost(path, body);
  const previous = (await entries.allEntries({ key: workspace }))[0];
  const replay = gateway({ ...batch, items: batch.items.map((item) => ({ ...item, request_id: item.request_id + 100 })) });
  await expect(apiPost(path, body)).rejects.toThrow(/protected retry identity/i);
  expect(replay.keys[0]).toBe(keys[0]);
  expect((await listLocalActionRetryEntries())[0]).toMatchObject({ batch_requests: previous.batch_requests });
});

it.each(["load", "update", "delete"])("retains a valid command read and protected batch after %s storage failure", async (operation) => {
  const warning = vi.spyOn(console, "warn").mockImplementation(() => {});
  gateway({ ...batch, items: [batch.items[0]] });
  await apiPost(path, { ...body, target_ids: [4], confirmation: "RUN ON 1 TARGETS" });
  const failure =
    operation === "load"
      ? vi.spyOn(entries, "allEntries")
      : operation === "update"
        ? vi.spyOn(entries, "updateEntryIfMatching")
        : vi.spyOn(entries, "deleteEntryIfMatching");
  failure.mockRejectedValueOnce(new Error("private-storage-failure-canary"));
  try {
    expect(await observe(71)).toEqual(detail(71));
    expect(await listLocalActionRetryEntries()).toHaveLength(1);
    expect(warning).toHaveBeenCalledWith(expect.stringContaining("could not be persisted"));
    expect(JSON.stringify(warning.mock.calls)).not.toContain("private-storage-failure-canary");
  } finally {
    failure.mockRestore();
    warning.mockRestore();
  }
});

it("settles an exact batch through the non-browser memory adapter", async () => {
  const browserWindow = window;
  vi.stubGlobal("window", undefined);
  try {
    gateway();
    await apiGet("/api/status");
    await apiPost(path, body);
    await observe(71);
    expect(await entries.allEntries({ key: workspace })).toHaveLength(1);
    await observe(72);
    expect(await entries.allEntries({ key: workspace })).toEqual([]);
  } finally {
    await resetRetryStorage();
    vi.stubGlobal("window", browserWindow);
  }
});
