import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useConsoleWorkspaceSession } from "./use-console-workspace-session";

function deferred() {
  let reject;
  const promise = new Promise((_, fail) => {
    reject = fail;
  });
  return { promise, reject };
}

function props(overrides = {}) {
  return {
    attachConsoleSession: vi.fn(),
    newConsoleSession: vi.fn(),
    onOpenConnectorOperation: vi.fn(() => false),
    restartConsoleSession: vi.fn(),
    runtimeSelectedSession: { id: 0, status: "idle", error: null },
    selectedRunningRequestID: null,
    selectedRuntimeTarget: null,
    selectedTarget: { ref: "postgres:1:1", connector_kind: "postgres" },
    selectedTargetUsesLiveConsole: false,
    sessions: [],
    resolveConnectorModel: vi.fn(() => null),
    ...overrides,
  };
}

describe("useConsoleWorkspaceSession", () => {
  it("keeps structured session state isolated by target", () => {
    const initial = props();
    const { result, rerender } = renderHook((value) => useConsoleWorkspaceSession(value), { initialProps: initial });
    expect(result.current.selectedStructuredSession.active).toBe(true);

    act(() => result.current.endStructured());
    expect(result.current.selectedStructuredSession.active).toBe(false);

    rerender({ ...initial, selectedTarget: { ref: "postgres:2:2", connector_kind: "postgres" } });
    expect(result.current.selectedStructuredSession.active).toBe(true);
    rerender(initial);
    expect(result.current.selectedStructuredSession.active).toBe(false);
  });

  it("selects named live sessions per target and attaches the selected session", () => {
    const attachConsoleSession = vi.fn();
    const value = props({
      attachConsoleSession,
      runtimeSelectedSession: { id: 12, runtime_id: 4, name: "second", status: "connected" },
      selectedRuntimeTarget: { id: 4, connector_kind: "ssh" },
      selectedTarget: { ref: "ssh:1:1", connector_kind: "ssh" },
      selectedTargetUsesLiveConsole: true,
      sessions: [
        { id: 11, runtime_id: 4, name: "first", status: "connected" },
        { id: 12, runtime_id: 4, name: "second", status: "connected" },
      ],
    });
    const { result } = renderHook(() => useConsoleWorkspaceSession(value));

    act(() => result.current.selectLiveSessionName("first"));

    expect(result.current.selectedSession.id).toBe(11);
    expect(attachConsoleSession).toHaveBeenLastCalledWith(11);
  });

  it("opens connector-owned recovery instead of leaking a session error", async () => {
    const operation = { open: true, connector_kind: "ssh", type: "host-key" };
    const onOpenConnectorOperation = vi.fn(() => true);
    const newConsoleSession = vi.fn().mockRejectedValue(new Error("fingerprint changed"));
    const runtimeTarget = { id: 4, connector_kind: "ssh" };
    const { result } = renderHook(() =>
      useConsoleWorkspaceSession(
        props({
          newConsoleSession,
          onOpenConnectorOperation,
          resolveConnectorModel: () => ({ operationFromError: () => operation }),
          selectedRuntimeTarget: runtimeTarget,
          selectedTarget: { ref: "ssh:1:1", connector_kind: "ssh" },
          selectedTargetUsesLiveConsole: true,
        }),
      ),
    );

    await act(async () => result.current.startNew(runtimeTarget));

    expect(onOpenConnectorOperation).toHaveBeenCalledWith(operation);
    expect(result.current.newSessionError).toBe("");
  });

  it("surfaces restart failures without dropping the selected runtime", async () => {
    const restartConsoleSession = vi.fn().mockRejectedValue(new Error("offline"));
    const { result } = renderHook(() =>
      useConsoleWorkspaceSession(props({ restartConsoleSession, selectedRuntimeTarget: { id: 4 }, selectedTargetUsesLiveConsole: true })),
    );

    await act(async () => result.current.restart());

    expect(restartConsoleSession).toHaveBeenCalledWith(4);
    expect(result.current.restartAction).toEqual({ state: "error", error: "offline" });
  });

  it("ignores a session failure after ownership moves to another target", async () => {
    const pending = deferred();
    const initial = props({
      newConsoleSession: vi.fn(() => pending.promise),
      selectedRuntimeTarget: { id: 4, connector_kind: "ssh" },
      selectedTarget: { ref: "ssh:1:1", connector_kind: "ssh" },
      selectedTargetUsesLiveConsole: true,
    });
    const { result, rerender } = renderHook((value) => useConsoleWorkspaceSession(value), { initialProps: initial });

    act(() => void result.current.startNew(initial.selectedRuntimeTarget));
    rerender({
      ...initial,
      selectedRuntimeTarget: { id: 5, connector_kind: "ssh" },
      selectedTarget: { ref: "ssh:2:2", connector_kind: "ssh" },
    });
    await act(async () => pending.reject(new Error("old target offline")));

    expect(result.current.newSessionError).toBe("");
    expect(initial.onOpenConnectorOperation).not.toHaveBeenCalled();
  });
});
