import { act, renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { useConsoleMessages } from "./use-console-messages";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts.ts";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPost: vi.fn() }));

function deferred() {
  let resolve: (_value: unknown) => void = () => {
    throw new Error("Deferred request is not initialized");
  };
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<unknown>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

type Props = Parameters<typeof useConsoleMessages>[0];
function baseProps(overrides: Partial<Props> = {}): Props {
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
    vi.mocked(apiGet).mockReset();
    vi.mocked(apiPost).mockReset();
  });

  it("ignores a message response from the previously selected runtime", async () => {
    const oldLoad = deferred();
    vi.mocked(apiGet)
      .mockReturnValueOnce(oldLoad.promise)
      .mockResolvedValueOnce([message(2, "current")]);
    const props = baseProps();
    const { result, rerender } = renderHook((value) => useConsoleMessages(value), { initialProps: props });

    act(() => void result.current.load());
    rerender({ ...props, selectedRuntimeTarget: { id: 4, name: "next" } });
    await act(async () => result.current.load());
    await act(async () => oldLoad.resolve([{ id: 1, message: "stale" }]));

    expect(result.current.state.data).toEqual([message(2, "current")]);
  });

  it("marks unread messages through the runtime-scoped gateway action when closed", () => {
    const markRuntimeMessagesRead = vi.fn().mockResolvedValue({});
    const { result } = renderHook(() =>
      useConsoleMessages(baseProps({ markRuntimeMessagesRead, selectedUnreadMessages: [message(1, "unread")] })),
    );

    act(() => result.current.open());
    act(() => result.current.close());

    expect(markRuntimeMessagesRead).toHaveBeenCalledWith(3);
  });

  it("ignores a pending list response after the drawer closes", async () => {
    const pending = deferred();
    vi.mocked(apiGet).mockReturnValue(pending.promise);
    const { result } = renderHook(() => useConsoleMessages(baseProps()));

    act(() => result.current.open());
    act(() => result.current.close());
    await act(async () => pending.resolve([{ id: 1, message: "late" }]));

    expect(result.current.isOpen).toBe(false);
    expect(result.current.state.data).toEqual([]);
  });

  it("sends to the captured runtime and refreshes both message views", async () => {
    vi.mocked(apiGet).mockResolvedValue([]);
    vi.mocked(apiPost).mockResolvedValue({});
    const props = baseProps();
    const { result } = renderHook(() => useConsoleMessages(props));
    act(() => result.current.setTokenID("5"));
    act(() => result.current.setText("Deploy completed"));

    await act(async () => result.current.submit({ preventDefault: vi.fn() }));

    expect(apiPost).toHaveBeenCalledWith(
      "/api/messages",
      {
        token_id: 5,
        runtime_id: 3,
        session_id: 9,
        direction: "user_to_ai",
        message: "Deploy completed",
      },
      { signal: expect.any(AbortSignal) },
    );
    expect(props.loadMessages).toHaveBeenCalledOnce();
    expect(result.current.text).toBe("");
  });

  it("does not let a send from a closed drawer erase a newer draft", async () => {
    const pendingSend = deferred();
    vi.mocked(apiGet).mockResolvedValue([]);
    vi.mocked(apiPost).mockReturnValue(pendingSend.promise);
    const { result } = renderHook(() => useConsoleMessages(baseProps()));
    act(() => {
      result.current.open();
      result.current.setTokenID("5");
      result.current.setText("old draft");
    });

    let submission: Promise<void> | undefined;
    act(() => {
      submission = result.current.submit({ preventDefault: vi.fn() });
    });
    act(() => {
      result.current.close();
      result.current.open();
      result.current.setText("new draft");
    });
    await act(async () => pendingSend.resolve({}));
    await submission;

    expect(result.current.text).toBe("new draft");
    expect(result.current.state.state).not.toBe("sending");
  });

  it("does not erase a draft edited while an earlier message is sending", async () => {
    const pendingSend = deferred();
    vi.mocked(apiGet).mockResolvedValue([]);
    vi.mocked(apiPost).mockReturnValue(pendingSend.promise);
    const { result } = renderHook(() => useConsoleMessages(baseProps()));
    act(() => {
      result.current.setTokenID("5");
      result.current.setText("same draft");
    });

    let submission: Promise<void> | undefined;
    act(() => {
      submission = result.current.submit({ preventDefault: vi.fn() });
    });
    act(() => {
      result.current.setText("new draft");
      result.current.setText("same draft");
    });
    await act(async () => pendingSend.resolve({}));
    await submission;

    expect(result.current.text).toBe("same draft");
  });

  it("closes and clears message state when the selected runtime changes", async () => {
    const pendingSend = deferred();
    vi.mocked(apiGet).mockResolvedValue([message(1, "old message")]);
    vi.mocked(apiPost).mockReturnValue(pendingSend.promise);
    const props = baseProps();
    const { result, rerender } = renderHook((value) => useConsoleMessages(value), { initialProps: props });
    act(() => {
      result.current.open();
      result.current.setTokenID("5");
      result.current.setText("old draft");
    });
    await act(async () => result.current.load());

    let submission: Promise<void> | undefined;
    act(() => {
      submission = result.current.submit({ preventDefault: vi.fn() });
    });
    rerender({ ...props, selectedRuntimeTarget: { id: 4, name: "next" } });

    expect(result.current.isOpen).toBe(false);
    expect(result.current.text).toBe("");
    expect(result.current.tokenID).toBe("");
    expect(result.current.state).toEqual({ state: "idle", data: [], error: null });

    await act(async () => pendingSend.resolve({}));
    await submission;
    expect(apiPost).toHaveBeenCalledWith(
      "/api/messages",
      expect.objectContaining({ runtime_id: 3, message: "old draft" }),
      expect.anything(),
    );
    expect(result.current.text).toBe("");
  });

  it("rejects a malformed current response without rendering unchecked message data", async () => {
    vi.mocked(apiGet).mockResolvedValue([{ id: 1, message: { secret: "not display data" } }]);
    const { result } = renderHook(() => useConsoleMessages(baseProps()));
    await act(async () => result.current.load());
    expect(result.current.state.state).toBe("error");
    expect(result.current.state.data).toEqual([]);
  });

  it("sends without a session identity when the runtime has no live session", async () => {
    vi.mocked(apiGet).mockResolvedValue([]);
    vi.mocked(apiPost).mockResolvedValue({});
    const { result } = renderHook(() => useConsoleMessages(baseProps({ selectedSession: {}, selectedSessionLive: false })));
    act(() => {
      result.current.setTokenID("5");
      result.current.setText("New note");
    });
    await act(async () => result.current.submit({ preventDefault: vi.fn() }));
    expect(apiPost).toHaveBeenCalledWith("/api/messages", expect.objectContaining({ session_id: null }), expect.anything());
  });

  it("preserves the draft and reports a current send failure", async () => {
    vi.mocked(apiPost).mockRejectedValue(new Error("Message rejected"));
    const { result } = renderHook(() => useConsoleMessages(baseProps()));
    act(() => {
      result.current.setTokenID("5");
      result.current.setText("Keep this draft");
    });
    await act(async () => result.current.submit({ preventDefault: vi.fn() }));
    expect(result.current.state).toEqual({ state: "error", data: [], error: "Message rejected" });
    expect(result.current.text).toBe("Keep this draft");
    expect(apiGet).not.toHaveBeenCalled();
  });

  it("reports a current mark-read failure without discarding loaded messages", async () => {
    const unread = message(1, "unread");
    vi.mocked(apiGet).mockResolvedValue([unread]);
    const markRuntimeMessagesRead = vi.fn().mockRejectedValue(new Error("Read status unavailable"));
    const { result } = renderHook(() => useConsoleMessages(baseProps({ markRuntimeMessagesRead, selectedUnreadMessages: [unread] })));
    await act(async () => result.current.load());
    await act(async () => result.current.close());
    expect(result.current.isOpen).toBe(false);
    expect(result.current.state).toEqual({ state: "error", data: [unread], error: "Read status unavailable" });
  });

  it("ignores a mark-read failure belonging to a previously selected runtime", async () => {
    const pending = deferred();
    const props = baseProps({
      markRuntimeMessagesRead: vi.fn().mockReturnValue(pending.promise),
      selectedUnreadMessages: [message(1, "unread")],
    });
    const { result, rerender } = renderHook((value) => useConsoleMessages(value), { initialProps: props });
    act(() => result.current.close());
    rerender({ ...props, selectedRuntimeTarget: { id: 4 } });
    await act(async () => {
      pending.reject(new Error("Old read status failed"));
      await pending.promise.catch(() => undefined);
    });
    expect(result.current.state).toEqual({ state: "idle", data: [], error: null });
  });

  it.each(["list", "send"])("ignores a delayed %s failure after the drawer closes", async (operation) => {
    const pending = deferred();
    vi.mocked(apiGet).mockReturnValue(pending.promise);
    vi.mocked(apiPost).mockReturnValue(pending.promise);
    const { result } = renderHook(() => useConsoleMessages(baseProps()));
    let request!: Promise<void>;
    act(() => {
      result.current.setTokenID("5");
      result.current.setText("Draft");
    });
    act(() => {
      request = operation === "list" ? result.current.load() : result.current.submit({ preventDefault: vi.fn() });
    });
    act(() => result.current.close());
    await act(async () => {
      pending.reject(new Error("Late failure"));
      await request;
    });
    expect(result.current.state.error).toBeNull();
    expect(result.current.text).toBe("Draft");
  });

  it.each([
    { label: "runtime", overrides: { selectedRuntimeTarget: null }, text: "Draft", tokenID: "5" },
    { label: "text", overrides: {}, text: "   ", tokenID: "5" },
    { label: "token", overrides: {}, text: "Draft", tokenID: "" },
  ])("does not submit without a $label", async ({ overrides, text, tokenID }) => {
    const { result } = renderHook(() => useConsoleMessages(baseProps(overrides)));
    act(() => {
      result.current.setTokenID(tokenID);
      result.current.setText(text);
    });
    const preventDefault = vi.fn();
    await act(async () => result.current.submit({ preventDefault }));
    expect(preventDefault).toHaveBeenCalledOnce();
    expect(apiPost).not.toHaveBeenCalled();
    expect(result.current.state.state).toBe("idle");
  });
});

function message(id: number, text: string): RuntimeMessage {
  return { id, token_id: 5, runtime_id: 3, direction: "ai_to_user", message: text, created_at: "2026-09-26" };
}
