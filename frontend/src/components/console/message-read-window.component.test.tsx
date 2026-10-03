import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts.ts";
import { useConsoleMessages } from "./use-console-messages";
import { MessagesDialog } from "./messages-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));
const acknowledge = vi.fn().mockResolvedValue({});
const tokens = [
  { id: 5, name: "Agent A" },
  { id: 6, name: "Agent B" },
];
function message(id: number, tokenID = 5, direction: RuntimeMessage["direction"] = "ai_to_user"): RuntimeMessage {
  return { id, token_id: tokenID, runtime_id: 3, direction, message: `Note ${id}`, created_at: "2026-10-03" };
}
function Fixture() {
  const messages = useConsoleMessages({
    loadMessages: vi.fn(),
    markRuntimeMessagesRead: acknowledge,
    selectedRuntimeTarget: { id: 3, name: "Fixture" },
    selectedSession: {},
    selectedSessionLive: false,
    selectedTokenOptions: tokens,
    selectedUnreadMessages: [message(1), message(2, 6)],
  });
  return (
    <>
      <button onClick={() => messages.open(5)}>Open notes</button>
      <MessagesDialog
        open={messages.isOpen}
        target={{ name: "Fixture" }}
        tokens={tokens}
        tokenID={messages.tokenID}
        state={messages.state}
        text={messages.text}
        onTokenChange={messages.setTokenID}
        onTextChange={messages.setText}
        onSubmit={messages.submit}
        onRefresh={messages.load}
        onClose={messages.close}
        onRendered={messages.recordDisplayed}
      />
    </>
  );
}
beforeEach(() => {
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiPost).mockReset();
  acknowledge.mockClear();
});

it("acknowledges the committed token-filtered window, not hidden tokens or outbound notes", async () => {
  vi.mocked(apiGet).mockResolvedValue([message(1), message(2, 6), message(3, 5, "user_to_ai")]);
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Open notes" }));
  expect(await screen.findByText("Note 1")).toBeVisible();
  expect(screen.queryByText("Note 2")).not.toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Close drawer" }));
  expect(acknowledge).toHaveBeenCalledExactlyOnceWith(3, [1]);
});

it("does not acknowledge an old window when a refresh is still pending", async () => {
  vi.mocked(apiGet)
    .mockResolvedValueOnce([message(1)])
    .mockReturnValueOnce(new Promise(() => {}));
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Open notes" }));
  await screen.findByText("Note 1");
  fireEvent.click(screen.getByRole("button", { name: "Refresh messages" }));
  fireEvent.click(screen.getByRole("button", { name: "Close drawer" }));
  expect(acknowledge).not.toHaveBeenCalled();
});

it.each(["pending", "failed", "malformed", "oversized", "duplicate"])("does not acknowledge an unseen %s response", async (mode) => {
  if (mode === "pending") vi.mocked(apiGet).mockReturnValue(new Promise(() => {}));
  else if (mode === "failed") vi.mocked(apiGet).mockRejectedValue(new Error("Unavailable"));
  else if (mode === "oversized") vi.mocked(apiGet).mockResolvedValue(Array.from({ length: 101 }, (_, index) => message(index + 1)));
  else if (mode === "duplicate") vi.mocked(apiGet).mockResolvedValue([message(1), message(1)]);
  else vi.mocked(apiGet).mockResolvedValue([{ message: "malformed" }]);
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Open notes" }));
  if (mode !== "pending") await screen.findByText(mode === "failed" ? "Unavailable" : "Invalid runtime messages response.");
  fireEvent.click(screen.getByRole("button", { name: "Close drawer" }));
  expect(acknowledge).not.toHaveBeenCalled();
});

it("uses the newly rendered token and refreshed window without acknowledging stale or consumed rows", async () => {
  vi.mocked(apiGet)
    .mockResolvedValueOnce([message(1), message(2, 6)])
    .mockResolvedValueOnce([message(4, 6), { ...message(5, 6), consumed_at: "2026-10-03" }]);
  render(<Fixture />);
  fireEvent.click(screen.getByRole("button", { name: "Open notes" }));
  await screen.findByText("Note 1");
  fireEvent.change(screen.getByRole("combobox"), { target: { value: "6" } });
  expect(screen.getByText("Note 2")).toBeVisible();
  fireEvent.click(screen.getByRole("button", { name: "Refresh messages" }));
  await screen.findByText("Note 4");
  fireEvent.click(screen.getByRole("button", { name: "Close drawer" }));
  await waitFor(() => expect(acknowledge).toHaveBeenCalledExactlyOnceWith(3, [4]));
});

function hookProps() {
  return {
    loadMessages: vi.fn(),
    markRuntimeMessagesRead: acknowledge,
    selectedRuntimeTarget: { id: 3, name: "Fixture" } as { id: number; name: string } | null,
    selectedSession: {},
    selectedSessionLive: true,
    selectedTokenOptions: tokens,
    selectedUnreadMessages: [],
  };
}

it("does not load a message window without a runtime", async () => {
  const { result } = renderHook(() => useConsoleMessages({ ...hookProps(), selectedRuntimeTarget: null }));
  await act(async () => result.current.load());
  expect(apiGet).not.toHaveBeenCalled();
  expect(result.current.state.state).toBe("idle");
});

it("rejects a retained close after switching runtimes without consuming the new window", async () => {
  vi.mocked(apiGet).mockResolvedValue([message(1)]);
  const props = hookProps();
  const { result, rerender } = renderHook((value) => useConsoleMessages(value), { initialProps: props });
  await act(async () => result.current.open(5));
  const oldClose = result.current.close;
  rerender({ ...props, selectedRuntimeTarget: { id: 4, name: "Next" } });
  vi.mocked(apiGet).mockResolvedValue([{ ...message(2), runtime_id: 4 }]);
  await act(async () => result.current.open(5));
  act(() => result.current.recordDisplayed([2]));
  act(oldClose);
  expect(result.current.isOpen).toBe(true);
  expect(acknowledge).not.toHaveBeenCalled();
  act(() => result.current.close());
  expect(acknowledge).toHaveBeenCalledExactlyOnceWith(4, [2]);
});

it("sends without inventing a session ID while a live session is still being created", async () => {
  vi.mocked(apiGet).mockResolvedValue([]);
  vi.mocked(apiPost).mockResolvedValue({});
  const { result } = renderHook(() => useConsoleMessages(hookProps()));
  await act(async () => result.current.open(5));
  act(() => result.current.setText("Session pending"));
  await act(async () => result.current.submit({ preventDefault: vi.fn() }));
  expect(apiPost).toHaveBeenCalledWith("/api/messages", expect.objectContaining({ session_id: null }), expect.anything());
});

it("does not let an old acknowledgement failure overwrite a reopened drawer", async () => {
  let reject!: (_error: Error) => void;
  const pending = new Promise((_, fail) => {
    reject = fail;
  });
  const markRead = vi.fn().mockReturnValue(pending);
  vi.mocked(apiGet).mockResolvedValue([message(1)]);
  const { result } = renderHook(() => useConsoleMessages({ ...hookProps(), markRuntimeMessagesRead: markRead }));
  await act(async () => result.current.open());
  act(() => result.current.recordDisplayed([1]));
  act(() => {
    result.current.close();
    result.current.close();
  });
  expect(markRead).toHaveBeenCalledExactlyOnceWith(3, [1]);
  await act(async () => result.current.open());
  await act(async () => {
    reject(new Error("Old acknowledgement failed"));
    await pending.catch(() => {});
  });
  expect(result.current.isOpen).toBe(true);
  expect(result.current.state.state).toBe("ready");
  expect(result.current.state.error).toBeNull();
});
