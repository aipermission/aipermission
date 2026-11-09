import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { scopedUICookieName } from "../../lib/ui-cookie.ts";

const workspaceHeader = "X-AIPermission-Workspace";
let api: typeof import("../../lib/api.ts");

beforeEach(async () => {
  vi.resetModules();
  api = await import("../../lib/api.ts");
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=current-workspace; path=/`;
  document.cookie = `${scopedUICookieName("aipermission_csrf")}=csrf-token; path=/`;
  vi.useFakeTimers();
});

afterEach(() => {
  document.cookie = `${scopedUICookieName("aipermission_workspace")}=; Max-Age=0; path=/`;
  document.cookie = `${scopedUICookieName("aipermission_csrf")}=; Max-Age=0; path=/`;
  vi.useRealTimers();
});

function jsonResponse(data: unknown = { ok: true }, workspace: string | null = "current-workspace", status = 200) {
  const headers = new Headers({ "Content-Type": "application/json", "X-AIPermission-Workspace-Changed": "true" });
  if (workspace !== null) headers.set(workspaceHeader, workspace);
  return new Response(JSON.stringify(data), { status, headers });
}

function deferred<T>() {
  let resolve!: (_value: T) => void;
  const promise = new Promise<T>((finish) => {
    resolve = finish;
  });
  return { promise, resolve };
}

function hangingFetch() {
  const mock = vi.fn((_url: RequestInfo | URL, { signal }: RequestInit) => {
    if (!signal) throw new Error("Expected request signal");
    return new Promise<Response>((_resolve, reject) => {
      if (signal.aborted) reject(signal.reason);
      else signal.addEventListener("abort", () => reject(signal.reason), { once: true });
    });
  });
  vi.stubGlobal("fetch", mock);
  return mock;
}

it.each(["GET", "PUT"] as const)("bounds a hanging %s with the shared request signal", async (method) => {
  const mock = hangingFetch();
  const parent = new AbortController();
  const remove = vi.spyOn(parent.signal, "removeEventListener");
  const pending =
    method === "GET"
      ? api.apiGet("/api/settings", { timeoutMs: 100, signal: parent.signal })
      : api.apiPut("/api/settings", {}, { timeoutMs: 100, signal: parent.signal });
  const rejected = expect(pending).rejects.toThrow(`Gateway ${method === "GET" ? "read" : "PUT"} timed out after 100ms.`);
  const signal = mock.mock.calls[0][1].signal!;
  expect(signal).not.toBe(parent.signal);
  await vi.advanceTimersByTimeAsync(99);
  expect(signal.aborted).toBe(false);
  await vi.advanceTimersByTimeAsync(1);
  await rejected;
  expect(signal.aborted).toBe(true);
  expect(parent.signal.aborted).toBe(false);
  expect(remove).toHaveBeenCalledWith("abort", expect.any(Function));
  expect(vi.getTimerCount()).toBe(0);
});

it.each([false, true])("preserves parent abort reasons (already canceled: %s) and cleans bounds", async (alreadyCanceled) => {
  const parent = new AbortController();
  const reason = new DOMException("Caller canceled PUT", "AbortError");
  const add = vi.spyOn(parent.signal, "addEventListener");
  const remove = vi.spyOn(parent.signal, "removeEventListener");
  if (alreadyCanceled) parent.abort(reason);
  hangingFetch();
  const rejected = expect(api.apiPut("/api/settings", {}, { signal: parent.signal, timeoutMs: 100 })).rejects.toBe(reason);
  if (!alreadyCanceled) parent.abort(reason);
  await rejected;
  if (alreadyCanceled) expect(add).not.toHaveBeenCalled();
  else expect(remove).toHaveBeenCalledWith("abort", add.mock.calls[0][1]);
  expect(vi.getTimerCount()).toBe(0);
});

it.each(["timeout", "parent", "unbounded parent"] as const)("rejects cancellation during PUT body consumption: %s", async (kind) => {
  const body = deferred<string>();
  const response = jsonResponse({}, "pinned-workspace");
  const readBody = vi.spyOn(response, "text").mockReturnValue(body.promise);
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response));
  const parent = new AbortController();
  const reason = new DOMException("Canceled during body", "AbortError");
  const pending = api.apiPut(
    "/api/settings",
    {},
    {
      workspaceBinding: "pinned-workspace",
      signal: parent.signal,
      ...(kind !== "unbounded parent" ? { timeoutMs: 100 } : {}),
    },
  );
  const rejected =
    kind === "timeout" ? expect(pending).rejects.toThrow("Gateway PUT timed out after 100ms.") : expect(pending).rejects.toBe(reason);
  await Promise.resolve();
  expect(readBody).toHaveBeenCalledOnce();
  if (kind === "timeout") await vi.advanceTimersByTimeAsync(100);
  else parent.abort(reason);
  // A body reader can still resolve after cancellation; it must not turn into success.
  body.resolve('{"ok":true}');
  await rejected;
  expect(api.currentWorkspaceBinding()).toBe("current-workspace");
  expect(vi.getTimerCount()).toBe(0);
});

it("bounds a hanging PUT response body as well as fetch", async () => {
  const parent = new AbortController();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (_url: RequestInfo | URL, { signal }: RequestInit) => {
      const response = jsonResponse();
      vi.spyOn(response, "text").mockImplementation(
        () =>
          new Promise((_resolve, reject) => {
            signal!.addEventListener("abort", () => reject(signal!.reason), { once: true });
          }),
      );
      return response;
    }),
  );
  const rejected = expect(api.apiPut("/api/settings", {}, { timeoutMs: 100, signal: parent.signal })).rejects.toThrow(
    "Gateway PUT timed out after 100ms.",
  );
  await vi.advanceTimersByTimeAsync(100);
  await rejected;
  expect(vi.getTimerCount()).toBe(0);
});

it.each(["", "   "])("rejects an empty explicit workspace %j without sending PUT", async (workspaceBinding) => {
  const mock = vi.fn();
  vi.stubGlobal("fetch", mock);
  await expect(api.apiPut("/api/settings", {}, { workspaceBinding, timeoutMs: 100 })).rejects.toThrow(
    "Gateway PUT workspace binding is required.",
  );
  expect(mock).not.toHaveBeenCalled();
  expect(vi.getTimerCount()).toBe(0);
});

it("sends the exact pinned workspace and never adopts it as the current workspace", async () => {
  const mock = vi.fn().mockResolvedValue(jsonResponse({ saved: true }, "Pinned-Workspace_42"));
  vi.stubGlobal("fetch", mock);
  await expect(api.apiPut("/api/settings", { enabled: true }, { workspaceBinding: "Pinned-Workspace_42" })).resolves.toEqual({
    saved: true,
  });
  const [url, options] = mock.mock.calls[0];
  expect(url).toBe(`${api.apiUrl}/api/settings`);
  expect(new Headers(options.headers).get(workspaceHeader)).toBe("Pinned-Workspace_42");
  expect(api.currentWorkspaceBinding()).toBe("current-workspace");
});

it.each([null, "foreign-workspace"])("rejects pinned PUT response workspace %j without switching workspaces", async (workspace) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({}, workspace)));
  const invalidate = vi.fn();
  window.addEventListener("aipermission:ui-session-required", invalidate);
  try {
    await expect(api.apiPut("/api/settings", {}, { workspaceBinding: "pinned-workspace" })).rejects.toThrow(
      "Gateway PUT workspace binding mismatch.",
    );
    expect(api.currentWorkspaceBinding()).toBe("current-workspace");
    expect(invalidate).not.toHaveBeenCalled();
  } finally {
    window.removeEventListener("aipermission:ui-session-required", invalidate);
  }
});

it.each([
  [401, "ui session required", null],
  [423, "database is locked", "foreign-workspace"],
] as const)("observes %s auth errors before rejecting a mismatched pinned response", async (status, error, workspace) => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(jsonResponse({ error }, workspace, status)));
  const invalidate = vi.fn();
  window.addEventListener("aipermission:ui-session-required", invalidate);
  try {
    const rejected = expect(api.apiPut("/api/settings", {}, { workspaceBinding: "pinned-workspace", timeoutMs: 100 })).rejects.toThrow(
      "Gateway PUT workspace binding mismatch.",
    );
    await rejected;
    expect(invalidate).toHaveBeenCalledOnce();
    expect(api.currentWorkspaceBinding()).toBe("current-workspace");
    expect(vi.getTimerCount()).toBe(0);
  } finally {
    window.removeEventListener("aipermission:ui-session-required", invalidate);
  }
});

it("retains legacy PUT headers, payload, signal, credentials and workspace capture", async () => {
  const parent = new AbortController();
  const mock = vi.fn().mockResolvedValue(jsonResponse({ saved: true }, "new-workspace"));
  vi.stubGlobal("fetch", mock);
  await expect(api.apiPut("/api/settings", { enabled: true }, { signal: parent.signal })).resolves.toEqual({ saved: true });
  expect(mock.mock.calls[0][1]).toEqual({
    method: "PUT",
    headers: { "Content-Type": "application/json", "X-AIPermission-CSRF": "csrf-token", [workspaceHeader]: "current-workspace" },
    body: '{"enabled":true}',
    signal: parent.signal,
    credentials: "include",
  });
  expect(api.currentWorkspaceBinding()).toBe("new-workspace");
  expect(vi.getTimerCount()).toBe(0);
});

it.each([undefined, 0, -1, NaN, Infinity])("preserves the caller signal for disabled timeout %s", async (timeoutMs) => {
  const parent = new AbortController();
  const mock = vi.fn().mockResolvedValue(jsonResponse());
  vi.stubGlobal("fetch", mock);
  await api.apiPut("/api/settings", {}, { signal: parent.signal, timeoutMs });
  expect(mock.mock.calls[0][1].signal).toBe(parent.signal);
  expect(vi.getTimerCount()).toBe(0);
});

it.each(["success", "fetch error", "HTTP error", "invalid JSON"])("cleans PUT timers and parent listeners after %s", async (outcome) => {
  const parent = new AbortController();
  const add = vi.spyOn(parent.signal, "addEventListener");
  const remove = vi.spyOn(parent.signal, "removeEventListener");
  const mock = vi
    .fn()
    .mockResolvedValue(
      outcome === "invalid JSON"
        ? new Response("not JSON", { headers: { [workspaceHeader]: "pinned-workspace" } })
        : jsonResponse({ error: "invalid setting" }, "pinned-workspace", outcome === "HTTP error" ? 422 : 200),
    );
  if (outcome === "fetch error") mock.mockRejectedValue(new TypeError("network unavailable"));
  vi.stubGlobal("fetch", mock);
  const pending = api.apiPut("/api/settings", {}, { timeoutMs: 100, signal: parent.signal, workspaceBinding: "pinned-workspace" });
  if (outcome === "success") await expect(pending).resolves.toEqual({ error: "invalid setting" });
  else if (outcome === "HTTP error") await expect(pending).rejects.toMatchObject({ status: 422, message: "invalid setting" });
  else await expect(pending).rejects.toThrow(outcome === "fetch error" ? "network unavailable" : "Invalid JSON response");
  expect(remove).toHaveBeenCalledWith("abort", add.mock.calls[0][1]);
  expect(vi.getTimerCount()).toBe(0);
  await vi.advanceTimersByTimeAsync(100);
  expect(mock.mock.calls[0][1].signal.aborted).toBe(false);
});
