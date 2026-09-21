import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useRedisBrowser } from "./use-redis-browser";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

let actionImplementation;

beforeEach(() => {
  actionImplementation = async ({ actionName, input }) => completed(actionName, responseFor(actionName, input));
  runGuardedConnectorAction.mockReset();
  runGuardedConnectorAction.mockImplementation(async (options) => {
    const request = options.requestGuard.begin(options.channel || options.actionName);
    const visibility = options.requestGuard.claimVisibility();
    options.setState({ state: options.busy, error: "", message: "" });
    try {
      const item = await actionImplementation(options);
      if (!request.isCurrent()) return null;
      if (visibility.isCurrent()) options.setState({ state: "idle", error: "", message: "" });
      return item;
    } catch (error) {
      if (request.isCurrent() && visibility.isCurrent()) options.setState({ state: "error", error: error.message, message: "" });
      throw error;
    } finally {
      request.complete();
    }
  });
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
    actionImplementation = ({ actionName, input }) => {
      if (actionName !== "get_key") return Promise.resolve(completed(actionName, responseFor(actionName, input)));
      return new Promise((resolve) => pending.set(input.key, resolve));
    };
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));

    act(() => void result.current.loadKey("alpha"));
    await waitFor(() => expect(pending.has("alpha")).toBe(true));
    act(() => void result.current.loadKey("beta"));
    await waitFor(() => expect(pending.has("beta")).toBe(true));
    await act(async () => pending.get("alpha")(completed("get_key", { key: "alpha", type: "string", value: "old", ttl_ms: -1 })));
    expect(result.current.keyResult).toBeNull();
    await act(async () => pending.get("beta")(completed("get_key", { key: "beta", type: "string", value: "current", ttl_ms: -1 })));

    expect(result.current.activeKey).toBe("beta");
    expect(result.current.valueDraft).toBe("current");
  });

  it("clears another key's draft when the selected key read fails", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    await act(async () => result.current.loadKey("alpha"));
    expect(result.current.canSaveString).toBe(true);

    apiPost.mockRejectedValueOnce(new Error("read failed"));
    await act(async () => result.current.loadKey("beta"));

    expect(result.current.activeKey).toBe("beta");
    expect(result.current.keyResult).toBeNull();
    expect(result.current.valueDraft).toBe("");
    expect(result.current.canSaveString).toBe(false);
    act(() => result.current.saveStringValue());
    expect(result.current.confirmDialog.open).toBe(false);
    expect(result.current.state.error).toBe("Reload the complete string value before saving it.");
  });

  it("keeps truncated string previews read-only", async () => {
    apiPost.mockImplementation(async (_path, payload) =>
      completed(
        payload.action_name,
        payload.action_name === "get_key"
          ? { key: payload.input.key, type: "string", value: "partial...[truncated]", ttl_ms: -1, truncated: true }
          : responseFor(payload.action_name, payload.input),
      ),
    );
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    await act(async () => result.current.loadKey("alpha"));

    expect(result.current.valueDraft).toBe("partial...[truncated]");
    expect(result.current.canSaveString).toBe(false);
    expect(result.current.editableString).toBe(false);
    act(() => result.current.saveStringValue());
    expect(result.current.confirmDialog.open).toBe(false);
  });

  it.each(["resolve", "reject"])("retires a pending read and restores idle state when New is selected (%s)", async (settlement) => {
    let settleRead;
    apiPost.mockImplementation((_path, payload) => {
      if (payload.action_name !== "get_key")
        return Promise.resolve(completed(payload.action_name, responseFor(payload.action_name, payload.input)));
      return new Promise((resolve, reject) => {
        settleRead =
          settlement === "resolve"
            ? () => resolve(completed("get_key", responseFor("get_key", payload.input)))
            : () => reject(new Error("late failure"));
      });
    });
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));

    act(() => void result.current.loadKey("alpha"));
    await waitFor(() => expect(result.current.state.state).toBe("reading"));
    act(() => result.current.startNewKey());
    expect(result.current.state.state).toBe("idle");
    expect(result.current.activeKey).toBe("");

    await act(async () => settleRead());
    expect(result.current.state).toEqual({ state: "idle", error: "", message: "" });
    expect(result.current.canSaveString).toBe(false);
  });

  it("owns read failures in browser state without rejecting the UI event", async () => {
    actionImplementation = async () => {
      throw new Error("Redis is unavailable");
    };
    const { result } = renderBrowser({ active: true });

    await waitFor(() => expect(result.current.state.error).toBe("Redis is unavailable"));
    await expect(result.current.scanKeys({ reset: true })).resolves.toBeUndefined();
  });

  it("validates writes before confirmation and binds an approved write to its captured key", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    runGuardedConnectorAction.mockClear();
    act(() => result.current.saveStringValue());
    expect(result.current.state.error).toBe("Key is required.");
    expect(result.current.confirmDialog.open).toBe(false);
    expect(runGuardedConnectorAction).not.toHaveBeenCalled();

    await act(async () => result.current.loadKey("alpha"));
    act(() => result.current.setValueDraft("replacement"));
    act(() => result.current.saveStringValue());
    await act(async () => result.current.loadKey("beta"));
    runGuardedConnectorAction.mockClear();
    await act(async () => result.current.confirmPendingAction());

    const write = runGuardedConnectorAction.mock.calls.find(([options]) => options.actionName === "set_string")?.[0];
    expect(write.input).toEqual({ key: "alpha", value: "replacement", ttl_seconds: 0 });
  });

  it("preserves whitespace in Redis key and string value identities", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    runGuardedConnectorAction.mockClear();

    act(() => {
      result.current.startNewKey();
      result.current.setNewKey(" padded key ");
      result.current.setNewValue("   ");
    });
    expect(result.current.canSaveString).toBe(true);
    act(() => result.current.saveStringValue());
    expect(result.current.confirmDialog.details).toContainEqual({ label: "Key", value: " padded key " });
    await act(async () => result.current.confirmPendingAction());

    const write = runGuardedConnectorAction.mock.calls.find(([options]) => options.actionName === "set_string")?.[0];
    expect(write.input).toEqual({ key: " padded key ", value: "   ", ttl_seconds: 0 });
  });

  it("guards TTL updates until the selected key result is current", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));

    act(() => result.current.updateTTL());
    expect(result.current.confirmDialog.open).toBe(false);
    await act(async () => result.current.loadKey("alpha"));
    act(() => result.current.setTTLDraft("30"));
    act(() => result.current.updateTTL());
    expect(result.current.confirmDialog.type).toBe("ttl");
    act(() => result.current.closeConfirmDialog());
    expect(result.current.confirmDialog.open).toBe(false);
  });
});

function completed(actionName, output) {
  return {
    id: 1,
    request_id: 1,
    status: "completed",
    target_ref: "redis:1:1",
    connector_kind: "redis",
    action_name: actionName,
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    output,
  };
}

function responseFor(actionName, input) {
  const outputs = {
    scan_keys: { keys: ["alpha", "beta"], next_cursor: "0" },
    get_key: { key: input.key, type: "string", value: input.key, ttl_ms: -1, truncated: false },
    set_string: { key: input.key },
  };
  return outputs[actionName] || {};
}
