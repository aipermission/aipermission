import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { MaintenanceConsolePanel } from "./maintenance-console-panel";

vi.mock("../../lib/api", () => ({
  apiPost: vi.fn(),
  apiUrl: "http://localhost:3210",
}));

vi.mock("../console/pty-console", () => ({
  PtyConsole: ({ session, onInput, onResize }) => (
    <div>
      <span>terminal:{session.status}</span>
      <button type="button" onClick={() => onInput("whoami\n")}>
        Send input
      </button>
      <button type="button" onClick={() => onResize(100, 40)}>
        Resize
      </button>
    </div>
  ),
}));

class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static instances = [];

  constructor(url) {
    this.url = url;
    this.readyState = FakeWebSocket.CONNECTING;
    this.send = vi.fn();
    FakeWebSocket.instances.push(this);
  }

  close() {
    this.readyState = 3;
    this.onclose?.();
  }
}

describe("MaintenanceConsolePanel", () => {
  beforeEach(() => {
    apiPost.mockReset();
    apiPost.mockResolvedValue({});
    FakeWebSocket.instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("opens the audited console and forwards terminal input only over an open socket", async () => {
    const user = userEvent.setup();
    render(<MaintenanceConsolePanel />);

    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const socket = FakeWebSocket.instances[0];
    expect(socket.url).toBe("ws://localhost:3210/api/settings/maintenance-console/attach");
    expect(screen.getByText("terminal:connecting")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Send input" }));
    expect(socket.send).not.toHaveBeenCalled();

    socket.readyState = FakeWebSocket.OPEN;
    socket.onmessage?.({ data: JSON.stringify({ type: "ready", status: "connected", shell: "/bin/sh" }) });
    expect(socket.send).toHaveBeenNthCalledWith(1, JSON.stringify({ type: "input", data: "whoami\n" }));
    await user.click(screen.getByRole("button", { name: "Send input" }));
    await user.click(screen.getByRole("button", { name: "Resize" }));
    expect(socket.send).toHaveBeenNthCalledWith(2, JSON.stringify({ type: "input", data: "whoami\n" }));
    expect(socket.send).toHaveBeenNthCalledWith(3, JSON.stringify({ type: "resize", cols: 100, rows: 40 }));
  });

  it("shows unexpected disconnects and preserves input until the replacement socket is ready", async () => {
    const user = userEvent.setup();
    render(<MaintenanceConsolePanel />);
    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    const first = FakeWebSocket.instances[0];
    first.readyState = FakeWebSocket.OPEN;
    first.onmessage?.({ data: JSON.stringify({ type: "ready", status: "connected" }) });
    first.close();
    expect(await screen.findByText("terminal:disconnected")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Send input" }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(2));
    const replacement = FakeWebSocket.instances[1];
    expect(replacement.send).not.toHaveBeenCalled();
    replacement.readyState = FakeWebSocket.OPEN;
    replacement.onmessage?.({ data: JSON.stringify({ type: "ready", status: "connected" }) });
    expect(replacement.send).toHaveBeenCalledWith(JSON.stringify({ type: "input", data: "whoami\n" }));
  });

  it("surfaces API and malformed websocket failures without trapping the dialog", async () => {
    const user = userEvent.setup();
    apiPost.mockRejectedValueOnce(new Error("console denied"));
    render(<MaintenanceConsolePanel />);

    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));
    expect(await screen.findByText("console denied")).toBeVisible();

    apiPost.mockResolvedValue({});
    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));
    FakeWebSocket.instances[0].onmessage?.({ data: "not json" });
    expect(await screen.findByText("Maintenance console returned an invalid message.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(apiPost).toHaveBeenLastCalledWith("/api/settings/maintenance-console/close", {});
  });

  it("closes the backend console when the panel unmounts", async () => {
    const user = userEvent.setup();
    const view = render(<MaintenanceConsolePanel />);
    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));

    view.unmount();

    await waitFor(() => expect(apiPost).toHaveBeenLastCalledWith("/api/settings/maintenance-console/close", {}));
    expect(FakeWebSocket.instances[0].readyState).toBe(3);
  });

  it("closes a delayed backend open that resolves after unmount", async () => {
    const user = userEvent.setup();
    let resolveOpen;
    const delayedOpen = new Promise((resolve) => {
      resolveOpen = resolve;
    });
    apiPost.mockReturnValueOnce(delayedOpen).mockResolvedValue({});
    const view = render(<MaintenanceConsolePanel />);
    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));

    view.unmount();
    await waitFor(() => expect(apiPost.mock.calls.filter(([path]) => path.endsWith("/close"))).toHaveLength(1));
    resolveOpen({});

    await waitFor(() => expect(apiPost.mock.calls.filter(([path]) => path.endsWith("/close"))).toHaveLength(2));
    expect(FakeWebSocket.instances).toHaveLength(0);
  });

  it("keeps a failed close retryable and reports the outcome-unknown session", async () => {
    const user = userEvent.setup();
    apiPost.mockResolvedValueOnce({}).mockRejectedValueOnce(new Error("close outcome unknown"));
    const view = render(<MaintenanceConsolePanel />);
    await user.click(screen.getByRole("button", { name: "Open maintenance console" }));
    await waitFor(() => expect(FakeWebSocket.instances).toHaveLength(1));

    await user.click(screen.getByRole("button", { name: "Close dialog" }));
    expect(await screen.findByText(/close outcome unknown/)).toBeVisible();
    view.unmount();

    await waitFor(() => expect(apiPost.mock.calls.filter(([path]) => path.endsWith("/close"))).toHaveLength(2));
  });
});
