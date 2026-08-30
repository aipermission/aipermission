import { act, renderHook } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useConsoleConnections } from "./use-console-connections";

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

function useHarness() {
  const [sessions, setConsoleSessions] = useState({
    state: "ready",
    data: [{ id: 7, status: "connected", transcript: "ready\r\n", error: null }],
    error: null,
  });
  return { sessions, connections: useConsoleConnections({ setConsoleSessions }) };
}

describe("useConsoleConnections", () => {
  beforeEach(() => {
    FakeWebSocket.instances = [];
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
});
