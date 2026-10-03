import { useState } from "react";
import type { ComponentProps } from "react";
import { Link, Outlet, useLocation } from "react-router";
import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import App from "./App";
import { apiGet } from "./lib/api";
import { errorMessage } from "./lib/errors.ts";
import { useUnlockStatus } from "./lib/use-unlock-status.ts";
import type { UnlockPage, UnlockShell } from "./pages/unlock.tsx";
import type { Shell } from "./components/app-shell.tsx";

const { setTheme, consoleModule, mcpModule } = vi.hoisted(() => {
  let resolve!: () => void;
  const ready = new Promise<void>((done) => {
    resolve = done;
  });
  return { setTheme: vi.fn(), consoleModule: { ready, resolve }, mcpModule: { loaded: false } };
});

vi.mock("./lib/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("./lib/api")>()), apiGet: vi.fn() }));
vi.mock("./lib/theme", () => ({ useTheme: () => ({ theme: "dark", setTheme }) }));
vi.mock("./pages/dashboard", () => ({ DashboardPage: () => <h1>Dashboard route</h1> }));
vi.mock("./pages/credentials", () => ({ CredentialsPage: () => <h1>Credentials route</h1> }));
vi.mock("./pages/connectors", () => ({ ConnectorsPage: () => <h1>Connectors route</h1> }));
vi.mock("./pages/projects", () => ({ ProjectsPage: () => <h1>Projects route</h1> }));
vi.mock("./pages/vault", () => ({ VaultPage: () => <h1>Vault route</h1> }));
vi.mock("./pages/history", () => ({ HistoryPage: () => <h1>History route</h1> }));
vi.mock("./pages/audit-logs", () => ({ AuditLogsPage: () => <h1>Audit logs route</h1> }));
vi.mock("./pages/tokens", () => ({ TokensPage: () => <h1>Tokens route</h1> }));
vi.mock("./pages/security", () => ({ SecurityPage: () => <h1>Security route</h1> }));
vi.mock("./pages/settings", () => ({ SettingsPage: () => <h1>Settings route</h1> }));
vi.mock("./pages/mcp-setup", () => {
  mcpModule.loaded = true;
  return { MCPSetupPage: () => <h1>MCP setup route</h1> };
});
vi.mock("./pages/console", async () => {
  await consoleModule.ready;
  return { ConsolePage: () => <h1>Console route</h1> };
});
vi.mock("./components/app-shell", () => ({
  Shell: ({ theme, setTheme }: ComponentProps<typeof Shell>) => {
    const location = useLocation();
    return (
      <div>
        <span>Unlocked workspace</span>
        <span>Theme: {theme}</span>
        <output aria-label="Current route">{location.pathname}</output>
        <Link to="/credentials">Credentials</Link>
        <Link to="/mcp-setup">MCP setup</Link>
        <button type="button" onClick={() => setTheme("light")}>
          Use light theme
        </button>
        <Outlet />
      </div>
    );
  },
}));
vi.mock("./pages/unlock", () => ({
  UnlockShell: ({ title, children }: ComponentProps<typeof UnlockShell>) => (
    <div>
      <span>{title}</span>
      {children}
    </div>
  ),
  UnlockPage: ({ status, onUnlocked }: ComponentProps<typeof UnlockPage>) => {
    const [error, setError] = useState("");
    return (
      <div>
        <span>Unlock state: {status?.state}</span>
        <button
          type="button"
          onClick={() =>
            Promise.resolve(onUnlocked(new AbortController().signal)).catch((failure: unknown) => setError(errorMessage(failure)))
          }
        >
          Refresh unlock status
        </button>
        {error ? <span>{error}</span> : null}
      </div>
    );
  },
}));

const get = vi.mocked(apiGet);
beforeEach(() => {
  get.mockReset();
  setTheme.mockReset();
  window.history.replaceState(null, "", "/");
});

it("loads MCP setup only when navigating to its route", async () => {
  const user = userEvent.setup();
  expect(mcpModule.loaded).toBe(false);
  get.mockResolvedValueOnce({ state: "unlocked", databases: [] });
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Dashboard route" })).toBeVisible();
  expect(mcpModule.loaded).toBe(false);

  await user.click(screen.getByRole("link", { name: "MCP setup" }));

  expect(await screen.findByRole("heading", { name: "MCP setup route" })).toBeVisible();
  expect(mcpModule.loaded).toBe(true);
  expect(screen.getByText("Unlocked workspace")).toBeVisible();
  expect(get).toHaveBeenCalledTimes(1);
});

it.each([
  ["/", "Dashboard route"],
  ["/credentials", "Credentials route"],
  ["/connectors", "Connectors route"],
  ["/projects", "Projects route"],
  ["/vault", "Vault route"],
  ["/history", "History route"],
  ["/audit-logs", "Audit logs route"],
  ["/tokens", "Tokens route"],
  ["/security", "Security route"],
  ["/settings", "Settings route"],
  ["/mcp-setup", "MCP setup route"],
])("renders %s inside the unlocked workspace", async (path, heading) => {
  window.history.replaceState(null, "", path);
  get.mockResolvedValueOnce({ state: "unlocked", databases: [] });

  render(<App />);

  expect(await screen.findByRole("heading", { name: heading })).toBeVisible();
  expect(screen.getByText("Unlocked workspace")).toBeVisible();
  expect(screen.getByLabelText("Current route").textContent).toBe(path);
});

it.each([
  ["/servers", "/connectors", "Connectors route"],
  ["/backup", "/settings", "Settings route"],
  ["/not-a-route", "/", "Dashboard route"],
])("replaces %s with %s", async (path, destination, heading) => {
  window.history.replaceState(null, "", path);
  const historyLength = window.history.length;
  get.mockResolvedValueOnce({ state: "unlocked", databases: [] });

  render(<App />);

  expect(await screen.findByRole("heading", { name: heading })).toBeVisible();
  expect(window.location.pathname).toBe(destination);
  expect(window.history.length).toBe(historyLength);
});

it("shows the console loading notice until the lazy page is available", async () => {
  window.history.replaceState(null, "", "/console");
  get.mockResolvedValueOnce({ state: "unlocked", databases: [] });

  render(<App />);

  expect(await screen.findByText("Loading console...")).toBeVisible();
  expect(screen.getByText("Unlocked workspace")).toBeVisible();
  expect(screen.queryByRole("heading", { name: "Console route" })).not.toBeInTheDocument();

  await act(async () => consoleModule.resolve());

  expect(await screen.findByRole("heading", { name: "Console route" })).toBeVisible();
  expect(screen.queryByText("Loading console...")).not.toBeInTheDocument();
  expect(window.location.pathname).toBe("/console");
});

it("passes the theme callback to the shell and navigates without rechecking unlock status", async () => {
  const user = userEvent.setup();
  get.mockResolvedValueOnce({ state: "unlocked", databases: [] });
  render(<App />);
  expect(await screen.findByRole("heading", { name: "Dashboard route" })).toBeVisible();
  expect(screen.getByText("Theme: dark")).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Use light theme" }));
  expect(setTheme).toHaveBeenCalledExactlyOnceWith("light");

  await user.click(screen.getByRole("link", { name: "Credentials" }));
  expect(await screen.findByRole("heading", { name: "Credentials route" })).toBeVisible();
  expect(screen.queryByRole("heading", { name: "Dashboard route" })).not.toBeInTheDocument();
  expect(window.location.pathname).toBe("/credentials");
  expect(apiGet).toHaveBeenCalledTimes(1);
});

it("opens the requested route after the unlock callback reconciles the status", async () => {
  const user = userEvent.setup();
  window.history.replaceState(null, "", "/vault");
  get.mockResolvedValueOnce({ state: "session_required", databases: [] }).mockResolvedValueOnce({ state: "unlocked", databases: [] });
  render(<App />);
  expect(await screen.findByText("Unlock state: session_required")).toBeVisible();
  expect(screen.queryByText("Unlocked workspace")).not.toBeInTheDocument();
  expect(screen.queryByRole("heading", { name: "Vault route" })).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Refresh unlock status" }));

  expect(await screen.findByRole("heading", { name: "Vault route" })).toBeVisible();
  expect(screen.queryByRole("button", { name: "Refresh unlock status" })).not.toBeInTheDocument();
  expect(window.location.pathname).toBe("/vault");
  expect(apiGet).toHaveBeenCalledTimes(2);
});

it("loads unlock status and forwards lifecycle cancellation to reconciliation", async () => {
  const user = userEvent.setup();
  get.mockResolvedValue({ state: "session_required", databases: [] });

  render(<App />);

  expect(screen.getByText("Checking encrypted database...")).toBeVisible();
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));

  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  expect(get.mock.calls[1]).toEqual(["/api/unlock/status", { signal: expect.any(AbortSignal), timeoutMs: 4000 }]);
});

it("keeps the unlock workflow mounted when lifecycle status reconciliation fails", async () => {
  const user = userEvent.setup();
  get.mockResolvedValueOnce({ state: "session_required", databases: [] }).mockRejectedValueOnce(new Error("Status unavailable"));

  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));

  expect(await screen.findByText("Status unavailable")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh unlock status" })).toBeVisible();
  expect(screen.queryByText("Gateway unavailable")).not.toBeInTheDocument();
});

it("ignores an older background status response after lifecycle reconciliation", async () => {
  const user = userEvent.setup();
  const background = deferred();
  const lifecycle = deferred();
  get
    .mockResolvedValueOnce({ state: "session_required", databases: [] })
    .mockReturnValueOnce(background.promise)
    .mockReturnValueOnce(lifecycle.promise);

  render(<App />);
  const refresh = await screen.findByRole("button", { name: "Refresh unlock status" });
  act(() => window.dispatchEvent(new Event("aipermission:ui-session-required")));
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(2));
  await user.click(refresh);
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(3));

  await act(async () => lifecycle.resolve({ state: "unlocked", databases: [] }));
  expect(await screen.findByText("Unlocked workspace")).toBeVisible();
  await act(async () => background.resolve({ state: "session_required", databases: [] }));
  expect(screen.getByText("Unlocked workspace")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Refresh unlock status" })).not.toBeInTheDocument();
});

function deferred() {
  let resolve!: (_value: unknown) => void;
  let reject!: (_error: unknown) => void;
  const promise = new Promise<unknown>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

it.each([
  { databases: [] },
  { state: "", databases: [] },
  { state: "unlocked", databases: [{ id: "db", name: "Default", unlocked: "yes" }] },
])("rejects malformed unlock status without exposing the workspace: %j", async (response) => {
  get.mockResolvedValueOnce(response);
  render(<App />);
  expect(await screen.findByText("Gateway unavailable")).toBeVisible();
  expect(screen.queryByText("Unlocked workspace")).not.toBeInTheDocument();
});

it("keeps the unlock form available after malformed lifecycle reconciliation", async () => {
  const user = userEvent.setup();
  get.mockResolvedValueOnce({ state: "session_required", databases: [] }).mockResolvedValueOnce({ state: true });
  render(<App />);
  await user.click(await screen.findByRole("button", { name: "Refresh unlock status" }));
  expect(await screen.findByText("Invalid database status response.")).toBeVisible();
  expect(screen.getByRole("button", { name: "Refresh unlock status" })).toBeVisible();
});

it.each([401, 423].flatMap((status) => [null, "absent", "", "foreign", "pinned"].map((binding) => ({ status, binding }))))(
  "unmounts sensitive views immediately on authorization failure %j",
  async ({ status, binding }) => {
    window.history.replaceState(null, "", "/tokens");
    const pendingStatus = deferred();
    get.mockResolvedValueOnce({ state: "unlocked", databases: [] }).mockReturnValueOnce(pendingStatus.promise);
    render(<App />);
    expect(await screen.findByRole("heading", { name: "Tokens route" })).toBeVisible();
    const actualAPI = await vi.importActual<typeof import("./lib/api")>("./lib/api");
    vi.spyOn(globalThis, "fetch").mockResolvedValueOnce(
      new Response(JSON.stringify({ error: status === 423 ? "database is locked" : "ui session required" }), {
        status,
        headers: {
          "Content-Type": "application/json",
          ...(binding === null || binding === "absent" ? {} : { "X-AIPermission-Workspace": binding }),
        },
      }),
    );

    await act(async () => {
      const request =
        binding === null ? actualAPI.apiGet("/api/tokens") : actualAPI.apiPost("/api/settings", {}, { workspaceBinding: "pinned" });
      if (binding === null || binding === "pinned") await expect(request).rejects.toMatchObject({ status });
      else await expect(request).rejects.toThrow(/workspace binding mismatch/);
    });

    expect(screen.queryByRole("heading", { name: "Tokens route" })).not.toBeInTheDocument();
    expect(screen.queryByText("Unlocked workspace")).not.toBeInTheDocument();
    expect(screen.getByText("Checking encrypted database...")).toBeVisible();
    await act(async () => pendingStatus.resolve({ state: "locked", databases: [] }));
    expect(await screen.findByText("Unlock state: locked")).toBeVisible();
  },
);

it.each(["focus", "visibility"])("rechecks the gateway on %s and clears a stale unlocked view", async (event) => {
  get.mockResolvedValueOnce({ state: "unlocked", databases: [] }).mockResolvedValueOnce({ state: "session_required", databases: [] });
  render(<App />);
  expect(await screen.findByText("Unlocked workspace")).toBeVisible();
  await act(async () => {
    if (event === "focus") window.dispatchEvent(new Event("focus"));
    else document.dispatchEvent(new Event("visibilitychange"));
  });
  expect(await screen.findByText("Unlock state: session_required")).toBeVisible();
  expect(screen.queryByText("Unlocked workspace")).not.toBeInTheDocument();
});

it("aborts replaced and unmounted status requests and ignores late unlocked replies", async () => {
  const first = deferred();
  const latest = deferred();
  get.mockReturnValueOnce(first.promise).mockReturnValueOnce(latest.promise);
  const { unmount } = render(<App />);
  const initialSignal = get.mock.calls[0][1]?.signal;
  act(() => window.dispatchEvent(new Event("aipermission:ui-session-required")));
  expect(initialSignal?.aborted).toBe(true);
  await act(async () => latest.resolve({ state: "locked", databases: [] }));
  await act(async () => first.resolve({ state: "unlocked", databases: [] }));
  expect(screen.queryByText("Unlocked workspace")).not.toBeInTheDocument();

  const unmounted = deferred();
  get.mockReturnValueOnce(unmounted.promise);
  act(() => window.dispatchEvent(new Event("aipermission:ui-session-required")));
  const unmountedSignal = get.mock.calls[2][1]?.signal;
  unmount();
  expect(unmountedSignal?.aborted).toBe(true);
  await act(async () => unmounted.resolve({ state: "unlocked", databases: [] }));
  act(() => window.dispatchEvent(new Event("focus")));
  expect(get).toHaveBeenCalledTimes(3);
});

it("joins caller cancellation and does not start an already canceled status reconciliation", async () => {
  get.mockResolvedValueOnce({ state: "session_required", databases: [] });
  const hook = renderHook(useUnlockStatus);
  await waitFor(() => expect(hook.result.current.unlock.state).toBe("ready"));
  const controller = new AbortController();
  controller.abort();
  await act(async () => hook.result.current.loadUnlockStatus(controller.signal));
  expect(get).toHaveBeenCalledTimes(1);

  const pendingStatus = deferred();
  get.mockReturnValueOnce(pendingStatus.promise);
  const active = new AbortController();
  let reconcile!: Promise<void>;
  act(() => {
    reconcile = hook.result.current.loadUnlockStatus(active.signal);
  });
  active.abort();
  expect(get.mock.calls[1][1]?.signal?.aborted).toBe(true);
  await act(async () => {
    pendingStatus.reject(new Error("late canceled failure"));
    await reconcile;
  });
  expect(hook.result.current.unlock).toMatchObject({ state: "ready", data: { state: "session_required" } });
});

it("coalesces focus and periodic checks, skips hidden tabs, and clears its timer on unmount", async () => {
  vi.useFakeTimers();
  try {
    get.mockResolvedValueOnce({ state: "unlocked", databases: [] });
    const hook = renderHook(useUnlockStatus);
    await act(async () => {});
    expect(hook.result.current.unlock).toMatchObject({ state: "ready", data: { state: "unlocked" } });
    const visibility = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    await act(async () => vi.advanceTimersByTime(30000));
    expect(get).toHaveBeenCalledTimes(1);
    visibility.mockReturnValue("visible");
    const pendingStatus = deferred();
    get.mockReturnValueOnce(pendingStatus.promise);
    await act(async () => vi.advanceTimersByTime(30000));
    act(() => window.dispatchEvent(new Event("focus")));
    await act(async () => vi.advanceTimersByTime(30000));
    expect(get).toHaveBeenCalledTimes(2);
    await act(async () => pendingStatus.resolve({ state: "session_required", databases: [] }));
    expect(hook.result.current.unlock).toMatchObject({ state: "ready", data: { state: "session_required" } });
    hook.unmount();
    await act(async () => vi.advanceTimersByTime(60000));
    expect(get).toHaveBeenCalledTimes(2);
  } finally {
    vi.useRealTimers();
  }
});
