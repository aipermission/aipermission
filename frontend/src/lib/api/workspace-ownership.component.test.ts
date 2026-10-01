import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, currentWorkspaceBinding } from "../api";
import { listLocalActionRetryEntries } from "../local-action-retry";
import { allEntries } from "../local-action-retry/entries";
import { mutationTestWorkspace as workspace, setupMutationRetryStorage } from "../../test/connector-mutation-test-state";

setupMutationRetryStorage();
beforeEach(async () => {
  vi.stubGlobal("fetch", async () => reply({}, workspace));
  await apiGet("/api/status");
});

function reply(value: unknown, binding: string | null, status = 200) {
  return new Response(JSON.stringify(value), {
    status,
    headers: { ...(binding === null ? {} : { "X-AIPermission-Workspace": binding }), "X-AIPermission-Workspace-Changed": "true" },
  });
}

it.each([null, "", "foreign-workspace"])("rejects a pinned POST reply with binding %j without adopting it", async (binding) => {
  vi.stubGlobal("fetch", async () => reply({ ok: true }, binding));
  await expect(apiPost("/api/settings", {}, { workspaceBinding: workspace })).rejects.toThrow(/workspace binding mismatch/);
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it("uses the caller's pinned binding for dispatch rather than the current workspace", async () => {
  const pinned = "previous-workspace";
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ ok: true }, pinned));
  vi.stubGlobal("fetch", fetch);
  await expect(apiPost("/api/settings", {}, { workspaceBinding: pinned })).resolves.toEqual({ ok: true });
  expect(new Headers(fetch.mock.calls[0]?.[1]?.headers).get("X-AIPermission-Workspace")).toBe(pinned);
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it("rejects an explicitly empty binding before dispatch", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(apiPost("/api/settings", {}, { workspaceBinding: "" })).rejects.toThrow(/workspace binding/);
  expect(fetch).not.toHaveBeenCalled();
});

it("owns uncertain retries under the explicit binding even while another workspace is current", async () => {
  const pinned = "previous-workspace";
  const keys: string[] = [];
  const fetch = vi.fn<typeof globalThis.fetch>().mockImplementation(async (_url, options) => {
    expect(new Headers(options?.headers).get("X-AIPermission-Workspace")).toBe(pinned);
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    throw new Error("reply lost");
  });
  vi.stubGlobal("fetch", fetch);
  const body = { target_ref: "example:3:11", action_name: "example_action", input: {}, reason: "verify captured retry owner" };
  for (let attempt = 0; attempt < 2; attempt++)
    await expect(apiPost("/api/connector-actions/local-run", body, { workspaceBinding: pinned })).rejects.toThrow("reply lost");
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
  expect(await allEntries({ key: pinned })).toEqual([expect.objectContaining({ key: keys[0], state: "pending" })]);
  expect(await allEntries({ key: workspace })).toEqual([]);
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it.each([400, 409, 503])("cannot retire a protected mutation using an unowned HTTP %s reply", async (status) => {
  const keys: string[] = [];
  vi.stubGlobal("fetch", async (_url: unknown, options?: RequestInit) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    return reply({ error: "foreign failure" }, "foreign-workspace", status);
  });
  const body = { target_ref: "example:3:11", action_name: "example_action", input: {}, reason: "uncertain reply ownership" };
  for (let attempt = 0; attempt < 2; attempt++)
    await expect(apiPost("/api/connector-actions/local-run", body, { workspaceBinding: workspace })).rejects.toThrow(
      /workspace binding mismatch/,
    );
  expect(keys[0]).toBeTruthy();
  expect(keys[1]).toBe(keys[0]);
  expect(await listLocalActionRetryEntries()).toEqual([expect.objectContaining({ key: keys[0], state: "pending" })]);
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it("keeps an owned validation failure definitive without changing workspace", async () => {
  vi.stubGlobal("fetch", async () => reply({ error: "invalid input" }, workspace, 422));
  await expect(apiPost("/api/settings", {}, { workspaceBinding: workspace })).rejects.toMatchObject({ status: 422 });
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it("retains ordinary unpinned POST workspace discovery", async () => {
  vi.stubGlobal("fetch", async () => reply({ ok: true }, "next-workspace"));
  await expect(apiPost("/api/unlock", {})).resolves.toEqual({ ok: true });
  expect(currentWorkspaceBinding()).toBe("next-workspace");
});
