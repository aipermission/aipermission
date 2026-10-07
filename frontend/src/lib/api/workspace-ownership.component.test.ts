import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost, currentWorkspaceBinding } from "../api";
import { listLocalActionRetryEntries } from "../local-action-retry";
import { allEntries } from "../local-action-retry/entries";
import { mutationTestWorkspace as workspace, setupMutationRetryStorage } from "../../test/connector-mutation-test-state";
import { scopedUICookieName } from "../ui-cookie";

setupMutationRetryStorage();
beforeEach(async () => {
  vi.stubGlobal("fetch", async () => reply({}, workspace));
  await apiPost("/api/unlock", {});
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

it.each([400, 401, 409, 423, 503])("cannot retire a protected mutation using an unowned HTTP %s reply", async (status) => {
  const keys: string[] = [];
  vi.stubGlobal("fetch", async (_url: unknown, options?: RequestInit) => {
    keys.push(JSON.parse(String(options?.body)).idempotency_key);
    return reply(
      { error: status === 401 ? "ui session required" : status === 423 ? "database is locked" : "foreign failure" },
      "foreign-workspace",
      status,
    );
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

it.each([null, "", "foreign-workspace"])("rejects ordinary read data from binding %j without adopting it", async (binding) => {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ rows: ["foreign data"] }, binding));
  vi.stubGlobal("fetch", fetch);
  await expect(apiGet("/api/history")).rejects.toThrow(/workspace binding mismatch/);
  expect(new Headers(fetch.mock.calls[0]?.[1]?.headers).get("X-AIPermission-Workspace")).toBe(workspace);
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it("allows an owned read without changing the tab binding", async () => {
  vi.stubGlobal("fetch", async () => reply({ rows: ["owned data"] }, workspace));
  await expect(apiGet("/api/history")).resolves.toEqual({ rows: ["owned data"] });
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it("rejects an explicitly empty read binding before dispatch", async () => {
  const fetch = vi.fn();
  vi.stubGlobal("fetch", fetch);
  await expect(apiGet("/api/history", { workspaceBinding: "" })).rejects.toThrow(/read workspace binding is required/);
  expect(fetch).not.toHaveBeenCalled();
});

it("rejects a foreign pinned read without invalidating the current tab", async () => {
  const invalidate = vi.fn();
  window.addEventListener("aipermission:ui-session-required", invalidate);
  try {
    vi.stubGlobal("fetch", async () => reply({ rows: [] }, "foreign-workspace"));
    await expect(apiGet("/api/history", { workspaceBinding: workspace })).rejects.toThrow(/workspace binding mismatch/);
    expect(invalidate).not.toHaveBeenCalled();
    expect(currentWorkspaceBinding()).toBe(workspace);
  } finally {
    window.removeEventListener("aipermission:ui-session-required", invalidate);
  }
});

it("keeps unlock discovery available without adopting a foreign workspace", async () => {
  const fetch = vi.fn<typeof globalThis.fetch>().mockResolvedValue(reply({ state: "session_required" }, "foreign-workspace"));
  vi.stubGlobal("fetch", fetch);
  await expect(apiGet("/api/unlock/status")).resolves.toEqual({ state: "session_required" });
  expect(new Headers(fetch.mock.calls[0]?.[1]?.headers).has("X-AIPermission-Workspace")).toBe(false);
  expect(currentWorkspaceBinding()).toBe(workspace);
});

it.each([false, true])("reloads foreign unlock discovery only while its request is current (canceled=%s)", async (cancel) => {
  const original = window;
  const reload = vi.fn();
  vi.stubGlobal(
    "window",
    new Proxy(original, {
      get: (target, key) => (key === "location" ? { ...original.location, reload } : Reflect.get(target, key, target)),
    }),
  );
  // Capture this window's tab binding before another client changes the cookie.
  expect(currentWorkspaceBinding()).toBe(workspace);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=foreign-workspace; path=/`;
  const controller = new AbortController();
  const response = reply({ state: "unlocked", database_id: "two" }, "foreign-workspace");
  const originalText = response.text.bind(response);
  response.text = async () => {
    const text = await originalText();
    if (cancel) queueMicrotask(() => controller.abort());
    return text;
  };
  vi.stubGlobal("fetch", async () => response);
  const invalidate = vi.fn();
  window.addEventListener("aipermission:ui-session-required", invalidate);
  await expect(apiGet("/api/unlock/status", { signal: controller.signal })).rejects.toThrow(cancel ? /aborted/ : /reload required/);
  expect(reload).toHaveBeenCalledTimes(cancel ? 0 : 1);
  expect(invalidate).toHaveBeenCalledTimes(cancel ? 0 : 1);
  window.removeEventListener("aipermission:ui-session-required", invalidate);
  expect(currentWorkspaceBinding()).toBe(workspace);
  vi.stubGlobal("window", original);
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=${workspace}; path=/`;
});
