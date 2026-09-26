import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useUnlockLifecycleMutation } from "./use-unlock-lifecycle-mutation.ts";

function deferred<Result>() {
  let resolve: (_value: Result) => void = () => {
    throw new Error("Deferred request is not initialized");
  };
  const promise = new Promise<Result>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

describe("Unlock lifecycle mutation ownership", () => {
  it("reconciles with the same cancellation signal and preserves the typed mutation result", async () => {
    const onUnlocked = vi.fn().mockResolvedValue(undefined);
    const execute = vi.fn().mockResolvedValue({ database_id: "test" });
    const { result } = renderHook(() => useUnlockLifecycleMutation(onUnlocked));
    let response: { database_id: string } | undefined;
    await act(async () => {
      response = await result.current.runMutation("create", execute);
    });
    expect(response).toEqual({ database_id: "test" });
    expect(onUnlocked).toHaveBeenCalledWith(execute.mock.calls[0][0]);
    expect(result.current.activeMutation).toBe("");
  });

  it("blocks concurrent database lifecycle operations until the owner completes", async () => {
    const pending = deferred<string>();
    const { result } = renderHook(() => useUnlockLifecycleMutation(vi.fn()));
    let running: Promise<string> | undefined;
    act(() => {
      running = result.current.runMutation("import", () => pending.promise);
    });
    expect(result.current.activeMutation).toBe("import");
    const second = vi.fn().mockResolvedValue("unexpected");
    await expect(result.current.runMutation("delete", second)).rejects.toThrow("Another database operation is already running.");
    expect(second).not.toHaveBeenCalled();
    await act(async () => pending.resolve("imported"));
    expect(await running).toBe("imported");
    expect(result.current.activeMutation).toBe("");
  });

  it.each(["execute", "reconcile"])("releases ownership after a failure during %s", async (stage) => {
    const onUnlocked = stage === "reconcile" ? vi.fn().mockRejectedValue(new Error("failed")) : vi.fn();
    const execute = stage === "execute" ? vi.fn().mockRejectedValue(new Error("failed")) : vi.fn().mockResolvedValue("done");
    const { result } = renderHook(() => useUnlockLifecycleMutation(onUnlocked));
    await act(async () => {
      await expect(result.current.runMutation("create", execute)).rejects.toThrow("failed");
    });
    expect(result.current.activeMutation).toBe("");
    if (stage === "execute") expect(onUnlocked).not.toHaveBeenCalled();
  });

  it("aborts an unmounted operation and skips status reconciliation after its late response", async () => {
    const pending = deferred<string>();
    const onUnlocked = vi.fn();
    const execute = vi.fn((_signal: AbortSignal) => pending.promise);
    const { result, unmount } = renderHook(() => useUnlockLifecycleMutation(onUnlocked));
    let running: Promise<string> | undefined;
    act(() => {
      running = result.current.runMutation("create", execute);
    });
    const signal = execute.mock.calls[0][0];
    unmount();
    expect(signal.aborted).toBe(true);
    await act(async () => pending.resolve("done"));
    await running;
    expect(onUnlocked).not.toHaveBeenCalled();
  });
});
