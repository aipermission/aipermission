import { act, renderHook, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { apiGet, apiPost } from "../../lib/api";
import { invalidateUISession } from "../../lib/ui-session-events";
import { report } from "../../test/console-capacity-fixtures";
import { useConsoleCapacity } from "./use-console-capacity";

vi.mock("../../lib/api", () => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiUrl: "http://localhost:8080",
  currentWorkspaceBinding: () => "workspace-a",
}));

class CapacitySocket {
  static instances: CapacitySocket[] = [];
  onmessage: ((_event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn();
  url: string;
  constructor(_url: string) {
    this.url = _url;
    CapacitySocket.instances.push(this);
  }
}

beforeEach(() => {
  vi.mocked(apiGet).mockReset();
  vi.mocked(apiPost).mockReset();
  CapacitySocket.instances = [];
  vi.stubGlobal("WebSocket", CapacitySocket);
});
afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("shares one socket, coalesces change notifications, and aborts on unmount", async () => {
  vi.useFakeTimers();
  let signal: AbortSignal | undefined;
  vi.mocked(apiGet)
    .mockResolvedValueOnce(report())
    .mockImplementation((_path, options) => {
      signal = options?.signal;
      return new Promise(() => {});
    });
  const view = renderHook(() => useConsoleCapacity());
  await act(async () => {
    await vi.advanceTimersByTimeAsync(1);
  });
  expect(view.result.current.data?.used).toBe(3);
  expect(apiGet).toHaveBeenCalledTimes(1);
  expect(CapacitySocket.instances).toHaveLength(1);
  expect(CapacitySocket.instances[0].url).toBe("ws://localhost:8080/api/console/capacity/attach?workspace=workspace-a");
  act(() => {
    CapacitySocket.instances[0].onmessage?.({ data: '{"revision":2}' });
    CapacitySocket.instances[0].onmessage?.({ data: '{"revision":3}' });
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(250);
  });
  expect(apiGet).toHaveBeenLastCalledWith("/api/console/capacity", {
    signal: expect.any(AbortSignal),
    timeoutMs: 4000,
    workspaceBinding: "workspace-a",
  });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10000);
  });
  expect(apiGet).toHaveBeenCalledTimes(2);
  view.unmount();
  expect(signal?.aborted).toBe(true);
  expect(CapacitySocket.instances[0].close).toHaveBeenCalledOnce();
});

it("does not expose late data after UI invalidation", async () => {
  let resolve!: (_value: unknown) => void;
  vi.mocked(apiGet).mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const view = renderHook(() => useConsoleCapacity());
  act(() => invalidateUISession());
  await act(async () => {
    resolve(report());
  });
  expect(view.result.current.data).toBeNull();
  expect(view.result.current.open).toBe(false);
  expect(apiGet).toHaveBeenCalledTimes(1);
});

it("pauses hidden tabs and resumes with a fresh snapshot", async () => {
  let visible = "visible";
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visible as DocumentVisibilityState);
  vi.mocked(apiGet).mockResolvedValue(report());
  const view = renderHook(() => useConsoleCapacity());
  await waitFor(() => expect(view.result.current.data).not.toBeNull());
  act(() => {
    visible = "hidden";
    document.dispatchEvent(new Event("visibilitychange"));
  });
  const calls = vi.mocked(apiGet).mock.calls.length;
  act(() => {
    visible = "visible";
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await waitFor(() => expect(apiGet).toHaveBeenCalledTimes(calls + 1));
  view.unmount();
});

it("submits one exact selection even on double click and never auto-retries mutations", async () => {
  let resolve!: (_value: unknown) => void;
  vi.mocked(apiGet).mockResolvedValue(report());
  vi.mocked(apiPost).mockImplementation(
    () =>
      new Promise((done) => {
        resolve = done;
      }),
  );
  const view = renderHook(() => useConsoleCapacity());
  await waitFor(() => expect(view.result.current.data).not.toBeNull());
  let pending!: Promise<void>;
  act(() => {
    pending = view.result.current.close([report().items[0]], 30);
    void view.result.current.close([report().items[0]], 30);
  });
  expect(apiPost).toHaveBeenCalledTimes(1);
  expect(apiPost).toHaveBeenCalledWith(
    "/api/console/capacity/close",
    { idle_minutes: 30, sessions: [{ id: 1, generation: 1, last_activity: report().items[0].last_activity }] },
    { signal: expect.any(AbortSignal), workspaceBinding: "workspace-a" },
  );
  await act(async () => {
    resolve({ closed: [1], skipped: [], uncertain: [] });
    await pending;
  });
  expect(view.result.current.message).toContain("1 closed");
  expect(view.result.current.saving).toBe(false);
});

it("blocks stale actions after an unknown cleanup until a verification read succeeds", async () => {
  vi.mocked(apiGet)
    .mockResolvedValueOnce(report())
    .mockImplementation(() => new Promise(() => {}));
  vi.mocked(apiPost).mockRejectedValue(new Error("Failed to fetch"));
  const view = renderHook(() => useConsoleCapacity());
  await waitFor(() => expect(view.result.current.data).not.toBeNull());
  await act(async () => {
    await view.result.current.close([report().items[0]], 30);
  });
  expect(view.result.current.data).toBeNull();
  expect(view.result.current.message).toMatch(/may already have run/);
  expect(apiPost).toHaveBeenCalledTimes(1);
});
