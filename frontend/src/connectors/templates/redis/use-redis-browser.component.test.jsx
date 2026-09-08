import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useRedisBrowser } from "./use-redis-browser";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

beforeEach(() => {
  apiPost.mockReset();
  apiPost.mockImplementation(async (_path, payload) => completed(payload.action_name, responseFor(payload.action_name, payload.input)));
});

function renderBrowser({ active = false } = {}) {
  const props = {
    target: { ref: "redis:1:1", connector_kind: "redis", config: {} },
    approvals: { data: [] },
    session: { active, startedAt: active ? "2026-09-07T12:00:00Z" : "" },
    onRefreshActivity: vi.fn(),
  };
  return { ...renderHook((next) => useRedisBrowser(next), { initialProps: props }), props };
}

describe("useRedisBrowser", () => {
  it("drops pending writes and stale status when the target changes", () => {
    const hook = renderBrowser();
    act(() => {
      hook.result.current.setNewKey("session:key");
      hook.result.current.setNewValue("value");
    });
    act(() => hook.result.current.saveStringValue());
    expect(hook.result.current.confirmDialog.open).toBe(true);

    hook.rerender({ ...hook.props, target: { ref: "redis:2:2", connector_kind: "redis", config: {} } });

    expect(hook.result.current.confirmDialog.open).toBe(false);
    expect(hook.result.current.state).toEqual({ state: "idle", error: "", message: "" });
    expect(hook.result.current.newKey).toBe("");
  });

  it("scans an active session and ignores a superseded key read", async () => {
    const pending = new Map();
    apiPost.mockImplementation((_path, payload) => {
      if (payload.action_name !== "get_key")
        return Promise.resolve(completed(payload.action_name, responseFor(payload.action_name, payload.input)));
      return new Promise((resolve) => pending.set(payload.input.key, resolve));
    });
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));

    act(() => void result.current.loadKey("alpha"));
    await waitFor(() => expect(pending.has("alpha")).toBe(true));
    act(() => void result.current.loadKey("beta"));
    await waitFor(() => expect(pending.has("beta")).toBe(true));
    await act(async () => pending.get("alpha")(completed("get_key", { type: "string", value: "old", ttl_ms: -1 })));
    expect(result.current.keyResult).toBeNull();
    await act(async () => pending.get("beta")(completed("get_key", { type: "string", value: "current", ttl_ms: -1 })));

    expect(result.current.activeKey).toBe("beta");
    expect(result.current.valueDraft).toBe("current");
  });

  it("owns read failures in browser state without rejecting the UI event", async () => {
    apiPost.mockRejectedValue(new Error("Redis is unavailable"));
    const { result } = renderBrowser({ active: true });

    await waitFor(() => expect(result.current.state.error).toBe("Redis is unavailable"));
    await expect(result.current.scanKeys({ reset: true })).resolves.toBeUndefined();
  });

  it("validates writes before confirmation and binds an approved write to its captured key", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    apiPost.mockClear();
    act(() => result.current.saveStringValue());
    expect(result.current.state.error).toBe("Key is required.");
    expect(result.current.confirmDialog.open).toBe(false);
    expect(apiPost).not.toHaveBeenCalled();

    await act(async () => result.current.loadKey("alpha"));
    act(() => result.current.setValueDraft("replacement"));
    act(() => result.current.saveStringValue());
    await act(async () => result.current.loadKey("beta"));
    apiPost.mockClear();
    await act(async () => result.current.confirmPendingAction());

    const write = apiPost.mock.calls.find(([, payload]) => payload.action_name === "set_string")?.[1];
    expect(write.input).toEqual({ key: "alpha", value: "replacement", ttl_seconds: 0 });
  });
});

function completed(actionName, output) {
  return { id: 1, status: "completed", action_name: actionName, output };
}

function responseFor(actionName, input) {
  const outputs = {
    scan_keys: { keys: ["alpha", "beta"], next_cursor: "0" },
    get_key: { type: "string", value: input.key, ttl_ms: -1 },
    set_string: { key: input.key },
  };
  return outputs[actionName] || {};
}
