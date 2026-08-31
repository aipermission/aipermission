import { describe, expect, it, vi } from "vitest";

import { runGuardedConnectorAction } from "./action-runner.js";
import { createRequestGuard } from "./request-guard.js";

function runnerOptions(overrides = {}) {
  const setState = vi.fn();
  return {
    setState,
    options: {
      requestGuard: createRequestGuard("target:1"),
      channel: "list",
      targetRef: "test:1:1",
      actionName: "list_items",
      reason: "test action",
      product: "Test",
      setState,
      ...overrides,
    },
  };
}

describe("runGuardedConnectorAction", () => {
  it("ignores a response after the target scope changes", async () => {
    let resolveResponse;
    const post = () => new Promise((resolve) => (resolveResponse = resolve));
    const { setState, options } = runnerOptions({ post });
    const result = runGuardedConnectorAction(options);

    options.requestGuard.setScope("target:2");
    resolveResponse({ status: "completed", output: { ok: true } });

    await expect(result).resolves.toBeNull();
    expect(setState).toHaveBeenCalledTimes(1);
  });

  it("keeps approval pending separate from completion and refreshes activity", async () => {
    const onRefreshActivity = vi.fn();
    const { setState, options } = runnerOptions({
      post: async () => ({ status: "approval_pending", display_text: "Waiting for approval" }),
      onRefreshActivity,
    });

    await expect(runGuardedConnectorAction(options)).resolves.toBeNull();
    await vi.waitFor(() => expect(onRefreshActivity).toHaveBeenCalledOnce());
    expect(setState).toHaveBeenLastCalledWith({ state: "idle", error: "", message: "Waiting for approval" });
  });

  it("rejects a failed HTTP 200 action result", async () => {
    const { setState, options } = runnerOptions({
      post: async () => ({ status: "failed", error: "remote failure" }),
    });

    await expect(runGuardedConnectorAction(options)).rejects.toThrow("remote failure");
    expect(setState).toHaveBeenLastCalledWith({ state: "error", error: "remote failure", message: "" });
  });

  it("safely reports a non-Error approval refresh failure", async () => {
    const { setState, options } = runnerOptions({
      post: async () => ({ status: "approval_pending", display_text: "Waiting for approval" }),
      onRefreshActivity: async () => Promise.reject(null),
    });

    await expect(runGuardedConnectorAction(options)).resolves.toBeNull();
    await vi.waitFor(() => expect(setState).toHaveBeenCalledTimes(3));
    expect(setState).toHaveBeenLastCalledWith({
      state: "idle",
      error: "Approval is pending, but activity refresh failed: unknown error",
      message: "Waiting for approval",
    });
  });

  it("reports activity refresh failure after a completed action", async () => {
    const { setState, options } = runnerOptions({
      post: async () => ({ status: "completed", output: { ok: true } }),
      onRefreshActivity: async () => Promise.reject("refresh unavailable"),
    });

    await expect(runGuardedConnectorAction(options)).resolves.toMatchObject({ status: "completed" });
    expect(setState).toHaveBeenLastCalledWith({
      state: "idle",
      error: "Action completed, but activity refresh failed: refresh unavailable",
      message: "",
    });
  });

  it("uses the product fallback for a null request rejection", async () => {
    const { setState, options } = runnerOptions({ post: async () => Promise.reject(null) });

    await expect(runGuardedConnectorAction(options)).rejects.toBeNull();
    expect(setState).toHaveBeenLastCalledWith({ state: "error", error: "Test action failed.", message: "" });
  });

  it("can suppress a request error without changing the original rejection", async () => {
    const failure = new Error("expected failure");
    const { setState, options } = runnerOptions({ post: async () => Promise.reject(failure), suppressError: true });

    await expect(runGuardedConnectorAction(options)).rejects.toBe(failure);
    expect(setState).toHaveBeenLastCalledWith({ state: "idle", error: "", message: "" });
  });
});
