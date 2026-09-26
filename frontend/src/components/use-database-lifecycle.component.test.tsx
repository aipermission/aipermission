import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../lib/api";
import { useDatabaseLifecycle } from "./use-database-lifecycle";

vi.mock("../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

function renderLifecycle(pollIsCurrent = () => true) {
  const disconnectAllConsoleSessions = vi.fn();
  const hook = renderHook(() => useDatabaseLifecycle({ disconnectAllConsoleSessions, pollIsCurrent }));
  return { ...hook, disconnectAllConsoleSessions };
}

describe("useDatabaseLifecycle", () => {
  beforeEach(() => {
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPost).mockReset();
  });

  it("ignores a database status response from a stale poll generation", async () => {
    vi.mocked(apiGet).mockResolvedValue({ database_id: "stale" });
    const { result } = renderLifecycle(() => false);

    await act(async () => result.current.loadStatus(1));

    expect(result.current.status).toEqual({ state: "loading", data: null, error: null });
    expect(apiGet).toHaveBeenCalledWith("/api/unlock/status", { signal: undefined, timeoutMs: 4000 });
  });

  it("ignores a status error from a stale poll generation", async () => {
    vi.mocked(apiGet).mockRejectedValue(new Error("stale failure"));
    const { result } = renderLifecycle(() => false);
    await act(async () => result.current.loadStatus(1));
    expect(result.current.status).toEqual({ state: "loading", data: null, error: null });
  });

  it("retains the last verified catalog when a later response is malformed", async () => {
    vi.mocked(apiGet)
      .mockResolvedValueOnce({ database_id: "one", databases: catalog(true, false) })
      .mockResolvedValueOnce({ database_id: "other", databases: [{ id: "other", unlocked: "yes" }] });
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());
    await act(async () => result.current.loadStatus());
    expect(result.current.status).toMatchObject({
      state: "error",
      data: { database_id: "one", databases: catalog(true, false) },
      error: "Invalid database catalog response.",
    });
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
  });

  it("asks which database to lock when multiple databases are unlocked", async () => {
    vi.mocked(apiGet).mockResolvedValue({ databases: catalog(true, true) });
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());

    act(() => result.current.requestLock());

    expect(result.current.lockDialog.open).toBe(true);
    expect(apiPost).not.toHaveBeenCalled();
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
  });

  it("keeps the last database snapshot through a transient poll failure", async () => {
    vi.mocked(apiGet)
      .mockResolvedValueOnce({ databases: catalog(true, true) })
      .mockRejectedValueOnce(new Error("status timeout"));
    const { result } = renderLifecycle();

    await act(async () => result.current.loadStatus(1));
    await act(async () => result.current.loadStatus(2));
    expect(result.current.status).toEqual({
      state: "error",
      data: { state: undefined, database_id: undefined, database_name: undefined, unlocked: undefined, databases: catalog(true, true) },
      error: "status timeout",
    });

    act(() => result.current.requestLock());
    expect(result.current.lockDialog.open).toBe(true);
    expect(apiPost).not.toHaveBeenCalled();
  });

  it("locks the current database directly when it is the only unlocked database", async () => {
    vi.mocked(apiGet).mockResolvedValue({ databases: catalog(true, false) });
    vi.mocked(apiPost).mockRejectedValue(new Error("lock failed"));
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());

    await act(async () => result.current.requestLock());

    expect(apiPost).toHaveBeenCalledWith("/api/lock", { scope: "current" });
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
    expect(result.current.lockDialog).toMatchObject({ open: true, state: "error", error: "lock failed" });
  });

  it("closes without switching when the current database is selected", async () => {
    vi.mocked(apiGet).mockResolvedValue({ database_id: "one", databases: [catalog(true, true)[0]] });
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());
    act(() => result.current.openSwitch());

    await act(async () => result.current.switchDatabase());

    expect(apiPost).not.toHaveBeenCalled();
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
    expect(result.current.switchDialog.open).toBe(false);
  });

  it("keeps the switch dialog open with the backend failure", async () => {
    vi.mocked(apiGet).mockResolvedValue({
      database_id: "one",
      databases: catalog(true, true),
    });
    vi.mocked(apiPost).mockRejectedValue(new Error("invalid password"));
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());
    act(() => result.current.openSwitch());
    act(() => result.current.setSwitchDialog((current) => ({ ...current, database_id: "two", password: "wrong" })));

    await act(async () => result.current.switchDatabase());

    expect(apiPost).toHaveBeenCalledWith("/api/databases/switch", { database_id: "two", password: "wrong" });
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
    expect(result.current.switchDialog).toMatchObject({ open: true, state: "error", error: "invalid password" });
  });

  it("shows status failures and resets abandoned switch and lock dialogs", async () => {
    vi.mocked(apiGet).mockRejectedValue(new Error("status unavailable"));
    vi.mocked(apiPost).mockRejectedValue(new Error("lock unavailable"));
    const { result } = renderLifecycle();

    await act(async () => result.current.loadStatus());
    expect(result.current.status).toEqual({ state: "error", data: null, error: "status unavailable" });

    act(() => result.current.openSwitch());
    expect(result.current.switchDialog).toMatchObject({ open: true, database_id: "" });
    act(() => result.current.closeSwitch());
    expect(result.current.switchDialog.open).toBe(false);

    await act(async () => result.current.requestLock());
    expect(result.current.lockDialog).toMatchObject({ open: true, state: "error", error: "lock unavailable" });
    act(() => result.current.closeLock());
    expect(result.current.lockDialog).toMatchObject({ open: false, state: "idle", error: null });
  });
});

function catalog(first: boolean, second: boolean) {
  return [
    { id: "one", name: "One", unlocked: first },
    { id: "two", name: "Two", unlocked: second },
  ];
}
