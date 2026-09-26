import { act, renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { runGuardedConnectorAction } from "../_shared/action-runner";
import { useRedisBrowser } from "./use-redis-browser";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { GuardedConnectorActionOptions } from "../_shared/action-runner";
import type { RedisBrowserProps } from "./browser-types";
import { errorMessage } from "../../../lib/errors";

vi.mock("../_shared/action-runner", () => ({ runGuardedConnectorAction: vi.fn() }));

type MockActionOptions = GuardedConnectorActionOptions & { input: Record<string, unknown> };
type ActionResolver = (_response: ConnectorActionResponse | null) => void;
let actionImplementation: (_options: MockActionOptions) => Promise<ConnectorActionResponse | null>;

beforeEach(() => {
  actionImplementation = async ({ actionName, input }) => completed(actionName, responseFor(actionName, input));
  vi.mocked(runGuardedConnectorAction).mockReset();
  vi.mocked(runGuardedConnectorAction).mockImplementation(async (options) => {
    const request = options.requestGuard.begin(options.channel || options.actionName);
    const visibility = options.requestGuard.claimVisibility();
    options.setState({ state: options.busy || "running", error: "", message: "" });
    try {
      const item = await actionImplementation({ ...options, input: options.input || {} });
      if (!request.isCurrent()) return null;
      if (visibility.isCurrent()) options.setState({ state: "idle", error: "", message: "" });
      return item;
    } catch (error) {
      if (request.isCurrent() && visibility.isCurrent()) options.setState({ state: "error", error: errorMessage(error), message: "" });
      throw error;
    } finally {
      request.complete();
    }
  });
});

function renderBrowser({ active = false } = {}) {
  const props: RedisBrowserProps = {
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
    const pending = new Map<string, ActionResolver>();
    actionImplementation = ({ actionName, input }) => {
      if (actionName !== "get_key") return Promise.resolve(completed(actionName, responseFor(actionName, input)));
      return new Promise<ConnectorActionResponse | null>((resolve) => pending.set(String(input.key), resolve));
    };
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));

    act(() => void result.current.loadKey("alpha"));
    await waitFor(() => expect(pending.has("alpha")).toBe(true));
    act(() => void result.current.loadKey("beta"));
    await waitFor(() => expect(pending.has("beta")).toBe(true));
    await act(async () => pending.get("alpha")?.(completed("get_key", { key: "alpha", type: "string", value: "old", ttl_ms: -1 })));
    expect(result.current.keyResult).toBeNull();
    await act(async () => pending.get("beta")?.(completed("get_key", { key: "beta", type: "string", value: "current", ttl_ms: -1 })));

    expect(result.current.activeKey).toBe("beta");
    expect(result.current.valueDraft).toBe("current");
  });

  it("clears another key's draft when the selected key read fails", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    await act(async () => result.current.loadKey("alpha"));
    expect(result.current.canSaveString).toBe(true);

    actionImplementation = async ({ actionName, input }) => {
      if (actionName === "get_key" && input.key === "beta") throw new Error("read failed");
      return completed(actionName, responseFor(actionName, input));
    };
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
    actionImplementation = async ({ actionName, input }) =>
      completed(
        actionName,
        actionName === "get_key"
          ? { key: input.key, type: "string", value: "partial...[truncated]", ttl_ms: -1, truncated: true }
          : responseFor(actionName, input),
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
    let settleRead: () => void = () => {
      throw new Error("Key read was not dispatched");
    };
    actionImplementation = ({ actionName, input }) => {
      if (actionName !== "get_key") return Promise.resolve(completed(actionName, responseFor(actionName, input)));
      return new Promise<ConnectorActionResponse | null>((resolve, reject) => {
        settleRead =
          settlement === "resolve"
            ? () => resolve(completed("get_key", responseFor("get_key", input)))
            : () => reject(new Error("late failure"));
      });
    };
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
    vi.mocked(runGuardedConnectorAction).mockClear();
    act(() => result.current.saveStringValue());
    expect(result.current.state.error).toBe("Key is required.");
    expect(result.current.confirmDialog.open).toBe(false);
    expect(runGuardedConnectorAction).not.toHaveBeenCalled();

    await act(async () => result.current.loadKey("alpha"));
    act(() => result.current.setValueDraft("replacement"));
    act(() => result.current.saveStringValue());
    await act(async () => result.current.loadKey("beta"));
    vi.mocked(runGuardedConnectorAction).mockClear();
    await act(async () => result.current.confirmPendingAction());

    const write = vi.mocked(runGuardedConnectorAction).mock.calls.find(([options]) => options.actionName === "set_string")?.[0];
    expect(write?.input).toEqual({ key: "alpha", value: "replacement", ttl_seconds: 0 });
  });

  it("preserves whitespace in Redis key and string value identities", async () => {
    const { result } = renderBrowser({ active: true });
    await waitFor(() => expect(result.current.keys).toEqual(["alpha", "beta"]));
    vi.mocked(runGuardedConnectorAction).mockClear();

    act(() => {
      result.current.startNewKey();
      result.current.setNewKey(" padded key ");
      result.current.setNewValue("   ");
    });
    expect(result.current.canSaveString).toBe(true);
    act(() => result.current.saveStringValue());
    expect(result.current.confirmDialog.details).toContainEqual({ label: "Key", value: " padded key " });
    await act(async () => result.current.confirmPendingAction());

    const write = vi.mocked(runGuardedConnectorAction).mock.calls.find(([options]) => options.actionName === "set_string")?.[0];
    expect(write?.input).toEqual({ key: " padded key ", value: "   ", ttl_seconds: 0 });
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

  it.each(["resolve", "reject"])("does not retire a newer target's confirmation after a stale write %s", async (settlement) => {
    const pending = new Map<string, { resolve: ActionResolver; reject: (_error: Error) => void }>();
    actionImplementation = ({ actionName, input }) =>
      new Promise<ConnectorActionResponse | null>((resolve, reject) => {
        if (actionName !== "set_string") {
          resolve(completed(actionName, responseFor(actionName, input)));
          return;
        }
        pending.set(String(input.key), { resolve, reject });
      });
    const hook = renderBrowser();
    act(() => {
      hook.result.current.setNewKey("old");
      hook.result.current.setNewValue("old value");
    });
    act(() => hook.result.current.saveStringValue());
    act(() => void hook.result.current.confirmPendingAction());
    await waitFor(() => expect(pending.has("old")).toBe(true));
    hook.rerender({ ...hook.props, target: { ref: "redis:2:2", config: {} } });
    act(() => {
      hook.result.current.setNewKey("current");
      hook.result.current.setNewValue("current value");
    });
    act(() => hook.result.current.saveStringValue());
    act(() => void hook.result.current.confirmPendingAction());
    await waitFor(() => expect(pending.has("current")).toBe(true));

    await act(async () => {
      if (settlement === "resolve") pending.get("old")?.resolve(completed("set_string", { key: "old" }));
      else pending.get("old")?.reject(new Error("Old target failed"));
    });
    expect(hook.result.current.confirmDialog.pending).toBe(true);
    expect(hook.result.current.confirmDialog.details).toContainEqual({ label: "Key", value: "current" });
    expect(hook.result.current.newValue).toBe("current value");
    expect(hook.result.current.confirmDialog.error).toBe("");
    await act(async () => pending.get("current")?.resolve(completed("set_string", { key: "current" })));
    expect(hook.result.current.confirmDialog.open).toBe(false);
  });

  it("does not dispatch the same confirmation twice before the pending state renders", async () => {
    let finish: ActionResolver = () => {
      throw new Error("Write was not dispatched");
    };
    actionImplementation = () =>
      new Promise<ConnectorActionResponse | null>((resolve) => {
        finish = resolve;
      });
    const { result } = renderBrowser();
    act(() => {
      result.current.setNewKey("key");
      result.current.setNewValue("value");
    });
    act(() => result.current.saveStringValue());
    act(() => {
      void result.current.confirmPendingAction();
      void result.current.confirmPendingAction();
    });
    expect(vi.mocked(runGuardedConnectorAction)).toHaveBeenCalledOnce();
    await act(async () => finish(completed("set_string", { key: "key" })));
  });
});

function completed(actionName: string, output: unknown): ConnectorActionResponse {
  return {
    request_id: 1,
    status: "completed",
    target_ref: "redis:1:1",
    connector_kind: "redis",
    action_name: actionName,
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    output,
  };
}

function responseFor(actionName: string, input: Record<string, unknown>): unknown {
  const outputs: Record<string, unknown> = {
    scan_keys: { keys: ["alpha", "beta"], next_cursor: "0" },
    get_key: { key: input.key, type: "string", value: input.key, ttl_ms: -1, truncated: false },
    set_string: { key: input.key },
  };
  return outputs[actionName] || {};
}
