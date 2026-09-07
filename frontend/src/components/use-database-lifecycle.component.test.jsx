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
    apiGet.mockReset();
    apiPost.mockReset();
  });

  it("ignores a database status response from a stale poll generation", async () => {
    apiGet.mockResolvedValue({ database_id: "stale" });
    const { result } = renderLifecycle(() => false);

    await act(async () => result.current.loadStatus(1));

    expect(result.current.status).toEqual({ state: "loading", data: null, error: null });
  });

  it("asks which database to lock when multiple databases are unlocked", async () => {
    apiGet.mockResolvedValue({ databases: [{ unlocked: true }, { unlocked: true }] });
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());

    act(() => result.current.requestLock());

    expect(result.current.lockDialog.open).toBe(true);
    expect(apiPost).not.toHaveBeenCalled();
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
  });

  it("locks the current database directly when it is the only unlocked database", async () => {
    apiGet.mockResolvedValue({ databases: [{ unlocked: true }, { unlocked: false }] });
    apiPost.mockRejectedValue(new Error("lock failed"));
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());

    await act(async () => result.current.requestLock());

    expect(apiPost).toHaveBeenCalledWith("/api/lock", { scope: "current" });
    expect(disconnectAllConsoleSessions).toHaveBeenCalledOnce();
    expect(result.current.lockDialog).toMatchObject({ open: false, state: "error", error: "lock failed" });
  });

  it("closes without switching when the current database is selected", async () => {
    apiGet.mockResolvedValue({ database_id: "one", databases: [{ id: "one", unlocked: true }] });
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());
    act(() => result.current.openSwitch());

    await act(async () => result.current.switchDatabase());

    expect(apiPost).not.toHaveBeenCalled();
    expect(disconnectAllConsoleSessions).not.toHaveBeenCalled();
    expect(result.current.switchDialog.open).toBe(false);
  });

  it("keeps the switch dialog open with the backend failure", async () => {
    apiGet.mockResolvedValue({
      database_id: "one",
      databases: [
        { id: "one", unlocked: true },
        { id: "two", unlocked: true },
      ],
    });
    apiPost.mockRejectedValue(new Error("invalid password"));
    const { result, disconnectAllConsoleSessions } = renderLifecycle();
    await act(async () => result.current.loadStatus());
    act(() => result.current.openSwitch());
    act(() => result.current.setSwitchDialog((current) => ({ ...current, database_id: "two", password: "wrong" })));

    await act(async () => result.current.switchDatabase());

    expect(apiPost).toHaveBeenCalledWith("/api/databases/switch", { database_id: "two", password: "wrong" });
    expect(disconnectAllConsoleSessions).toHaveBeenCalledOnce();
    expect(result.current.switchDialog).toMatchObject({ open: true, state: "error", error: "invalid password" });
  });
});
