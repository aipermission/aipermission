import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { idleActionState, useAsyncAction } from "./use-async-action";

describe("useAsyncAction", () => {
  it("exposes pending state and resolves with a success message", async () => {
    let resolveAction;
    const action = new Promise((resolve) => {
      resolveAction = resolve;
    });
    const { result } = renderHook(() => useAsyncAction());

    let run;
    act(() => {
      run = result.current.runAction({ pending: "uploading", successMessage: (value) => `Saved ${value}`, action: () => action });
    });
    expect(result.current.actionState).toEqual({ state: "uploading", error: null, message: null });

    await act(async () => resolveAction("file"));
    await expect(run).resolves.toBe("file");
    expect(result.current.actionState).toEqual({ state: "idle", error: null, message: "Saved file" });
  });

  it("captures errors and can reset for a retry", async () => {
    const { result } = renderHook(() => useAsyncAction());

    await act(async () => {
      const value = await result.current.runAction({ action: async () => Promise.reject(new Error("Connection failed")) });
      expect(value).toBeUndefined();
    });
    expect(result.current.actionState).toEqual({ state: "error", error: "Connection failed", message: null });

    act(() => result.current.resetAction());
    expect(result.current.actionState).toEqual(idleActionState);
  });
});
