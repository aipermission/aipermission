import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useConsoleRecoveryState } from "./use-console-recovery-state";

afterEach(() => {
  vi.useRealTimers();
});

describe("useConsoleRecoveryState", () => {
  it("selects only connector-owned recoverable actions for the active target", () => {
    const selectedTarget = { connector_kind: "ssh", ref: "ssh:4:8" };
    const approvals = [
      { id: 1, status: "running", target_ref: "ssh:4:7", action_name: "exec" },
      { id: 2, status: "running", target_ref: "ssh:4:8", action_name: "unrelated" },
      { id: 3, status: "running", target_ref: "ssh:4:8", action_name: "exec" },
    ];
    const { result } = renderHook(() => useConsoleRecoveryState({ approvals, selectedTarget }));

    expect(result.current.runningRequest?.id).toBe(3);
  });

  it("updates elapsed-time state on a bounded interval and stops after unmount", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-08-01T12:00:00Z"));
    const { result, unmount } = renderHook(() => useConsoleRecoveryState({ approvals: [], selectedTarget: null, tickInterval: 1000 }));

    expect(result.current.now).toBe(Date.parse("2026-08-01T12:00:00Z"));
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.now).toBe(Date.parse("2026-08-01T12:00:01Z"));

    unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
