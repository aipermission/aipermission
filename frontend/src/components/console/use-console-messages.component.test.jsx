import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useConsoleMessages } from "./use-console-messages";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function baseProps(overrides = {}) {
  return {
    loadMessages: vi.fn(),
    markRuntimeMessagesRead: vi.fn(),
    selectedRuntimeTarget: { id: 3, name: "server" },
    selectedSession: { id: 9 },
    selectedSessionLive: true,
    selectedTokenOptions: [{ id: 5, name: "codex" }],
    selectedUnreadMessages: [],
    ...overrides,
  };
}

describe("useConsoleMessages", () => {
  beforeEach(() => {
    apiGet.mockReset();
    apiPost.mockReset();
  });

  it("ignores a message response from the previously selected runtime", async () => {
    const oldLoad = deferred();
    apiGet.mockReturnValueOnce(oldLoad.promise).mockResolvedValueOnce([{ id: 2, message: "current" }]);
    const props = baseProps();
    const { result, rerender } = renderHook((value) => useConsoleMessages(value), { initialProps: props });

    act(() => void result.current.load());
    rerender({ ...props, selectedRuntimeTarget: { id: 4, name: "next" } });
    await act(async () => result.current.load());
    await act(async () => oldLoad.resolve([{ id: 1, message: "stale" }]));

    expect(result.current.state.data).toEqual([{ id: 2, message: "current" }]);
  });

  it("marks unread messages through the runtime-scoped gateway action when closed", () => {
    const markRuntimeMessagesRead = vi.fn().mockResolvedValue({});
    const { result } = renderHook(() =>
      useConsoleMessages(baseProps({ markRuntimeMessagesRead, selectedUnreadMessages: [{ id: 1, runtime_id: 3 }] })),
    );

    act(() => result.current.open());
    act(() => result.current.close());

    expect(markRuntimeMessagesRead).toHaveBeenCalledWith(3);
  });

  it("ignores a pending list response after the drawer closes", async () => {
    const pending = deferred();
    apiGet.mockReturnValue(pending.promise);
    const { result } = renderHook(() => useConsoleMessages(baseProps()));

    act(() => result.current.open());
    act(() => result.current.close());
    await act(async () => pending.resolve([{ id: 1, message: "late" }]));

    expect(result.current.isOpen).toBe(false);
    expect(result.current.state.data).toEqual([]);
  });

  it("sends to the captured runtime and refreshes both message views", async () => {
    apiGet.mockResolvedValue([]);
    apiPost.mockResolvedValue({});
    const props = baseProps();
    const { result } = renderHook(() => useConsoleMessages(props));
    act(() => result.current.setTokenID("5"));
    act(() => result.current.setText("Deploy completed"));

    await act(async () => result.current.submit({ preventDefault: vi.fn() }));

    expect(apiPost).toHaveBeenCalledWith("/api/messages", {
      token_id: 5,
      runtime_id: 3,
      session_id: 9,
      direction: "user_to_ai",
      message: "Deploy completed",
    });
    expect(props.loadMessages).toHaveBeenCalledOnce();
    expect(result.current.text).toBe("");
  });
});
