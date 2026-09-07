import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useConsoleConnections } from "./use-console-connections";
import { useConsoleSessionCoordinator } from "./use-console-session-coordinator";

vi.mock("../../lib/api", async (importOriginal) => ({
  ...(await importOriginal()),
  apiGet: vi.fn(),
  apiPost: vi.fn(),
}));
vi.mock("./use-console-connections", () => ({ useConsoleConnections: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function renderCoordinator() {
  const connections = {
    attachSession: vi.fn(),
    closeSession: vi.fn(),
    disconnectAll: vi.fn(),
    disconnectSessions: vi.fn(),
    resizeSession: vi.fn(),
    sendInput: vi.fn(),
  };
  useConsoleConnections.mockReturnValue(connections);
  const hook = renderHook(() => useConsoleSessionCoordinator({ pollIsCurrent: () => true }));
  return { ...hook, connections };
}

const runtime = { id: 7, name: "My Server" };
const vaultOptions = { supported: true, items: [{ id: 1 }], defaults: [] };

describe("useConsoleSessionCoordinator", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    apiGet.mockReset();
    apiPost.mockReset();
    useConsoleConnections.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("creates and activates a plain console with the stable request contract", async () => {
    apiGet.mockResolvedValue({ supported: false });
    apiPost.mockResolvedValue({ id: 10, runtime_id: 7, status: "connecting" });
    const { result, connections } = renderCoordinator();

    await act(async () => result.current.newSession(runtime, { params: { container: "api" } }));
    await act(async () => vi.runAllTimersAsync());

    expect(apiPost).toHaveBeenCalledWith(
      "/api/console/sessions",
      {
        runtime_id: 7,
        name: "My Server shell",
        close_existing: true,
        params: { container: "api" },
        vault_items: undefined,
      },
      { signal: expect.any(AbortSignal) },
    );
    expect(connections.attachSession).toHaveBeenCalledWith(10);
  });

  it("does not retry a failed session creation as though the Vault probe failed", async () => {
    apiGet.mockResolvedValue({ supported: false });
    apiPost.mockRejectedValue(new Error("session creation failed"));
    const { result } = renderCoordinator();

    await act(async () => {
      await expect(result.current.newSession(runtime)).rejects.toThrow("session creation failed");
    });

    expect(apiPost).toHaveBeenCalledOnce();
  });

  it("does not open Vault selection from a superseded probe for the same runtime", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce({ supported: false });
    apiPost.mockResolvedValue({ id: 11, runtime_id: 7, status: "connecting" });
    const { result } = renderCoordinator();

    let oldStart;
    await act(async () => {
      oldStart = result.current.newSession(runtime);
      await result.current.newSession(runtime);
    });
    await act(async () => older.resolve(vaultOptions));
    expect(await oldStart).toBeNull();

    expect(result.current.vaultDialog.open).toBe(false);
    expect(apiPost).toHaveBeenCalledOnce();
  });

  it("does not let an older runtime probe replace the current Vault selection", async () => {
    const older = deferred();
    apiGet.mockReturnValueOnce(older.promise).mockResolvedValueOnce(vaultOptions);
    const { result } = renderCoordinator();
    const secondRuntime = { id: 8, name: "Second Server" };

    let firstSelection;
    let secondSelection;
    act(() => {
      firstSelection = result.current.newSession(runtime);
      secondSelection = result.current.newSession(secondRuntime);
    });
    await act(async () => Promise.resolve());
    expect(result.current.vaultDialog.runtime).toEqual(secondRuntime);

    await act(async () => older.resolve(vaultOptions));
    await expect(firstSelection).resolves.toBeNull();
    expect(result.current.vaultDialog.runtime).toEqual(secondRuntime);

    act(() => result.current.closeVaultDialog());
    await expect(secondSelection).resolves.toBeNull();
  });

  it("resolves a pending Vault selection when the dialog is dismissed", async () => {
    apiGet.mockResolvedValue(vaultOptions);
    const { result } = renderCoordinator();

    let pending;
    act(() => {
      pending = result.current.newSession(runtime);
    });
    await act(async () => Promise.resolve());
    expect(result.current.vaultDialog.open).toBe(true);
    act(() => result.current.closeVaultDialog());

    await expect(pending).resolves.toBeNull();
    expect(result.current.vaultDialog.open).toBe(false);
    expect(apiPost).not.toHaveBeenCalled();
  });

  it("does not activate a Vault session that completes after the dialog closes", async () => {
    const created = deferred();
    apiGet.mockResolvedValue(vaultOptions);
    apiPost.mockReturnValue(created.promise);
    const { result, connections } = renderCoordinator();

    act(() => {
      void result.current.newSession(runtime);
    });
    await act(async () => Promise.resolve());
    act(() => {
      void result.current.startVaultSession([{ item_id: 1 }]);
    });
    act(() => result.current.closeVaultDialog());
    await act(async () => created.resolve({ id: 12, runtime_id: 7, status: "connecting" }));
    await act(async () => vi.runAllTimersAsync());

    expect(connections.attachSession).not.toHaveBeenCalled();
    expect(result.current.vaultDialog.open).toBe(false);
  });

  it("keeps a newer runtime dialog when an older Vault session completes", async () => {
    const created = deferred();
    apiGet.mockResolvedValue(vaultOptions);
    apiPost.mockReturnValue(created.promise);
    const { result, connections } = renderCoordinator();

    let firstSelection;
    act(() => {
      firstSelection = result.current.newSession(runtime);
    });
    await act(async () => Promise.resolve());
    act(() => {
      void result.current.startVaultSession([{ item_id: 1 }]);
    });

    const secondRuntime = { id: 8, name: "Second Server" };
    let secondSelection;
    act(() => {
      secondSelection = result.current.newSession(secondRuntime);
    });
    await act(async () => Promise.resolve());
    await expect(firstSelection).resolves.toBeNull();
    expect(result.current.vaultDialog.runtime).toEqual(secondRuntime);

    await act(async () => created.resolve({ id: 12, runtime_id: 7, status: "connecting" }));
    await act(async () => vi.runAllTimersAsync());

    expect(result.current.vaultDialog.runtime).toEqual(secondRuntime);
    expect(connections.attachSession).not.toHaveBeenCalled();
    act(() => result.current.closeVaultDialog());
    await expect(secondSelection).resolves.toBeNull();
  });

  it("resolves pending Vault selection on unmount", async () => {
    apiGet.mockResolvedValue(vaultOptions);
    const { result, unmount } = renderCoordinator();
    let pending;
    act(() => {
      pending = result.current.newSession(runtime);
    });
    await act(async () => Promise.resolve());

    unmount();

    await expect(pending).resolves.toBeNull();
  });

  it("does not activate a plain session that completes after unmount", async () => {
    const created = deferred();
    apiGet.mockResolvedValue({ supported: false });
    apiPost.mockReturnValue(created.promise);
    const { result, unmount, connections } = renderCoordinator();

    let pending;
    act(() => {
      pending = result.current.newSession(runtime);
    });
    await act(async () => Promise.resolve());
    unmount();
    await act(async () => created.resolve({ id: 13, runtime_id: 7, status: "connecting" }));
    await expect(pending).resolves.toBeNull();
    await act(async () => vi.runAllTimersAsync());

    expect(connections.attachSession).not.toHaveBeenCalled();
  });

  it("disconnects only affected sessions before restarting a runtime", async () => {
    apiGet
      .mockResolvedValueOnce([
        { id: 10, runtime_id: 7, status: "connected" },
        { id: 20, runtime_id: 8, status: "connected" },
      ])
      .mockResolvedValueOnce([]);
    apiPost.mockResolvedValue({ status: "completed" });
    const { result, connections } = renderCoordinator();
    await act(async () => result.current.loadSessions());

    await act(async () => result.current.restartRuntime(7));

    expect(connections.disconnectSessions).toHaveBeenCalledWith([10]);
    expect(apiPost).toHaveBeenCalledWith("/api/console/runtime-surfaces/7/restart", {});
    expect(result.current.sessions.data).toEqual([]);
  });
});
