import { act, renderHook } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { useConsoleConnections } from "./use-console-connections";

vi.mock("../../lib/api", async (importOriginal) => ({
  ...(await importOriginal()),
  apiPost: vi.fn(),
}));

class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = FakeWebSocket.CONNECTING;
    FakeWebSocket.instances.push(this);
  }

  close() {
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  }

  send = vi.fn();
}

function useHarness(initialSession = { id: 7, status: "connected", transcript: "ready\r\n", error: null }) {
  const [sessions, setConsoleSessions] = useState({
    state: "ready",
    data: [initialSession],
    error: null,
  });
  return { sessions, connections: useConsoleConnections({ setConsoleSessions }) };
}

describe("useConsoleConnections", () => {
  beforeEach(() => {
    FakeWebSocket.instances = [];
    apiPost.mockReset();
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("keeps the session alive and emits a bounded warning for malformed frames", () => {
    const { result, unmount } = renderHook(() => useHarness());

    act(() => result.current.connections.attachSession(7));
    const socket = FakeWebSocket.instances[0];
    act(() => socket.onmessage({ data: "{" }));

    expect(result.current.sessions.data[0]).toMatchObject({
      status: "connecting",
      error: "Ignored malformed console server message.",
    });
    expect(result.current.sessions.data[0].transcript).toContain("[console protocol warning] Ignored malformed server message.");
    expect(socket.readyState).toBe(FakeWebSocket.CONNECTING);

    unmount();
    expect(socket.readyState).toBe(FakeWebSocket.CLOSED);
  });

  it("marks a live session failed when its socket closes unexpectedly", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.connections.attachSession(7));
    const socket = FakeWebSocket.instances[0];
    act(() => {
      socket.readyState = FakeWebSocket.CLOSED;
      socket.onclose();
    });

    expect(result.current.sessions.data[0]).toMatchObject({
      status: "error",
      error: "Console connection closed unexpectedly. Reconnect to continue.",
    });
  });

  it("preserves a terminal exit state when the socket closes afterward", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.connections.attachSession(7));
    const socket = FakeWebSocket.instances[0];
    act(() => socket.onmessage({ data: JSON.stringify({ type: "exit", status: "closed", data: "Remote shell exited." }) }));
    act(() => {
      socket.readyState = FakeWebSocket.CLOSED;
      socket.onclose();
    });

    expect(result.current.sessions.data[0]).toMatchObject({ status: "closed", error: "Remote shell exited." });
  });

  it("ignores the replaced socket close while forcing a reconnect", () => {
    const { result } = renderHook(() => useHarness());

    act(() => result.current.connections.attachSession(7));
    const first = FakeWebSocket.instances[0];
    act(() => result.current.connections.attachSession(7, { force: true }));

    expect(first.readyState).toBe(FakeWebSocket.CLOSED);
    expect(FakeWebSocket.instances).toHaveLength(2);
    expect(result.current.sessions.data[0]).toMatchObject({ status: "connecting", error: null });
  });

  it("keeps a user-closed session closed when its socket closes", async () => {
    apiPost.mockResolvedValue({});
    const { result } = renderHook(() => useHarness());
    act(() => result.current.connections.attachSession(7));

    await act(async () => result.current.connections.closeSession(7));

    expect(apiPost).toHaveBeenCalledWith("/api/console/sessions/7/close", {});
    expect(FakeWebSocket.instances[0].readyState).toBe(FakeWebSocket.CLOSED);
    expect(result.current.sessions.data[0]).toMatchObject({ status: "closed", error: null });
  });

  it("reports a failed close when the socket disappears before the API rejects", async () => {
    let rejectClose;
    apiPost.mockImplementation(
      () =>
        new Promise((_resolve, reject) => {
          rejectClose = reject;
        }),
    );
    const { result } = renderHook(() => useHarness());
    act(() => result.current.connections.attachSession(7));

    let closePromise;
    act(() => {
      closePromise = result.current.connections.closeSession(7);
    });
    const socket = FakeWebSocket.instances[0];
    act(() => {
      socket.readyState = FakeWebSocket.CLOSED;
      socket.onclose();
    });
    expect(result.current.sessions.data[0]).toMatchObject({ status: "connecting", error: null });

    await act(async () => {
      rejectClose(new Error("close endpoint unavailable"));
      await expect(closePromise).rejects.toThrow("close endpoint unavailable");
    });

    expect(result.current.sessions.data[0]).toMatchObject({
      status: "error",
      error: "Console connection closed before the session could be ended: close endpoint unavailable",
    });
  });

  it("reports a failed close while the current socket remains open", async () => {
    apiPost.mockRejectedValue(new Error("close endpoint unavailable"));
    const { result } = renderHook(() => useHarness());
    act(() => result.current.connections.attachSession(7));
    const socket = FakeWebSocket.instances[0];
    socket.readyState = FakeWebSocket.OPEN;

    await act(async () => {
      await expect(result.current.connections.closeSession(7)).rejects.toThrow("close endpoint unavailable");
    });

    expect(socket.readyState).toBe(FakeWebSocket.OPEN);
    expect(result.current.sessions.data[0]).toMatchObject({
      status: "error",
      error: "Failed to end console session: close endpoint unavailable",
    });
  });

  it("commits a close when the socket disappears before the API succeeds", async () => {
    let resolveClose;
    apiPost.mockImplementation(
      () =>
        new Promise((resolve) => {
          resolveClose = resolve;
        }),
    );
    const { result } = renderHook(() => useHarness());
    act(() => result.current.connections.attachSession(7));

    let closePromise;
    act(() => {
      closePromise = result.current.connections.closeSession(7);
    });
    const socket = FakeWebSocket.instances[0];
    act(() => {
      socket.readyState = FakeWebSocket.CLOSED;
      socket.onclose();
    });

    await act(async () => {
      resolveClose({});
      await closePromise;
    });

    expect(result.current.sessions.data[0]).toMatchObject({ status: "closed", error: null });
  });
});
