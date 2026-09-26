import { describe, expect, it, vi } from "vitest";

import { connectorActionBusy } from "./action-state.ts";
import { runGuardedConnectorAction } from "./action-runner";
import type { GuardedConnectorActionOptions } from "./action-runner";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { createRequestGuard } from "../../../lib/request-guard";

function deferred() {
  let resolve: (_value: unknown) => void = () => {};
  const promise = new Promise<unknown>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

function runnerOptions(overrides: Partial<GuardedConnectorActionOptions> = {}) {
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

function actionResponse(overrides: Partial<ConnectorActionResponse> = {}): ConnectorActionResponse {
  return {
    request_id: 1,
    target_ref: "test:1:1",
    connector_kind: "test",
    action_name: "list_items",
    retry_policy: { class: "read_only", guidance: "Safe to retry." },
    status: "completed",
    ...overrides,
  };
}

describe("runGuardedConnectorAction", () => {
  it("distinguishes retriable failures from active and uncertain actions", () => {
    expect(connectorActionBusy({ state: "idle" })).toBe(false);
    expect(connectorActionBusy({ state: "error" })).toBe(false);
    expect(connectorActionBusy({ state: "loading" })).toBe(true);
    const retriableFailure = { state: "error", retryBlocked: true };
    expect(connectorActionBusy(retriableFailure)).toBe(false);
    expect(connectorActionBusy(null)).toBe(true);
  });

  it("keeps a newer channel's visible state when an older channel completes", async () => {
    const list = deferred();
    const detail = deferred();
    const requestGuard = createRequestGuard("target:1");
    const setState = vi.fn();
    const first = runGuardedConnectorAction({
      ...runnerOptions().options,
      requestGuard,
      channel: "list",
      busy: "loading-list",
      setState,
      post: () => list.promise,
    });
    const second = runGuardedConnectorAction({
      ...runnerOptions().options,
      requestGuard,
      channel: "detail",
      busy: "loading-detail",
      setState,
      post: () => detail.promise,
    });

    list.resolve(actionResponse({ output: { list: true } }));
    await expect(first).resolves.toMatchObject({ status: "completed" });
    expect(setState).toHaveBeenLastCalledWith({ state: "loading-detail", error: "", message: "" });

    detail.resolve(actionResponse({ output: { detail: true }, display_text: "Detail ready" }));
    await expect(second).resolves.toMatchObject({ status: "completed" });
    expect(setState).toHaveBeenLastCalledWith({ state: "idle", error: "", message: "Detail ready" });
  });

  it("ignores a response after the target scope changes", async () => {
    const response = deferred();
    const signals: AbortSignal[] = [];
    const post: NonNullable<GuardedConnectorActionOptions["post"]> = (_path, _body, options) => {
      signals.push(options.signal);
      return response.promise;
    };
    const { setState, options } = runnerOptions({ post });
    const result = runGuardedConnectorAction(options);

    options.requestGuard.setScope("target:2");
    expect(signals[0].aborted).toBe(true);
    response.resolve(actionResponse({ output: { ok: true } }));

    await expect(result).resolves.toBeNull();
    expect(setState).toHaveBeenCalledTimes(1);
  });

  it("keeps approval pending separate from completion and refreshes activity", async () => {
    const onRefreshActivity = vi.fn();
    const onPending = vi.fn();
    const { setState, options } = runnerOptions({
      post: async () => actionResponse({ request_id: 42, status: "approval_pending", display_text: "Waiting for approval" }),
      onRefreshActivity,
      onPending,
    });

    await expect(runGuardedConnectorAction(options)).resolves.toBeNull();
    await vi.waitFor(() => expect(onRefreshActivity).toHaveBeenCalledOnce());
    expect(onPending).toHaveBeenCalledWith(expect.objectContaining({ request_id: 42, status: "approval_pending" }));
    expect(setState).toHaveBeenLastCalledWith({ state: "idle", error: "", message: "Waiting for approval" });
  });

  it("rejects a failed HTTP 200 action result", async () => {
    const { setState, options } = runnerOptions({
      post: async () => actionResponse({ status: "failed", error: "remote failure" }),
    });

    await expect(runGuardedConnectorAction(options)).rejects.toThrow("remote failure");
    expect(setState).toHaveBeenLastCalledWith({ state: "error", error: "remote failure", message: "" });
  });

  it("safely reports a non-Error approval refresh failure", async () => {
    const { setState, options } = runnerOptions({
      post: async () => actionResponse({ request_id: 44, status: "approval_pending", display_text: "Waiting for approval" }),
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
      post: async () => actionResponse({ output: { ok: true } }),
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

  it("surfaces structured uncertain outcomes and refreshes activity", async () => {
    const onRefreshActivity = vi.fn();
    const failure = Object.assign(new Error("persistence uncertain"), {
      data: {
        status: "outcome_unknown",
        request_id: 91,
        error: "The action may have completed.",
        assistant_hint: "Inspect external state before retrying.",
      },
    });
    const { setState, options } = runnerOptions({ post: async () => Promise.reject(failure), onRefreshActivity });

    await expect(runGuardedConnectorAction(options)).rejects.toBe(failure);
    expect(onRefreshActivity).toHaveBeenCalledOnce();
    expect(setState).toHaveBeenLastCalledWith({
      state: "error",
      error: "The action may have completed. Request 91. Inspect external state before retrying.",
      message: "",
    });
  });
});

it("returns the completed result and calls the completion handler even when activity refresh fails", async () => {
  const item = actionResponse({ output: { affected: 1 }, display_text: "Server message" });
  const onCompleted = vi.fn();
  const successMessage = vi.fn(() => "Saved locally");
  const { setState, options } = runnerOptions({
    post: async () => item,
    onCompleted,
    successMessage,
    onRefreshActivity: async () => Promise.reject(new Error("Activity unavailable")),
  });
  await expect(runGuardedConnectorAction(options)).resolves.toEqual(item);
  expect(onCompleted).toHaveBeenCalledExactlyOnceWith(item);
  expect(successMessage).toHaveBeenCalledExactlyOnceWith(item);
  expect(setState).toHaveBeenLastCalledWith({
    state: "idle",
    error: "Action completed, but activity refresh failed: Activity unavailable",
    message: "Saved locally",
  });
});

it("uses the product approval message without dispatching completion", async () => {
  const onCompleted = vi.fn();
  const { setState, options } = runnerOptions({
    post: async () => actionResponse({ status: "approval_pending" }),
    onCompleted,
  });
  await expect(runGuardedConnectorAction(options)).resolves.toBeNull();
  expect(onCompleted).not.toHaveBeenCalled();
  expect(setState).toHaveBeenLastCalledWith({ state: "idle", error: "", message: "Test action is awaiting approval." });
});

it("does not overwrite a new scope when a pending approval refresh fails late", async () => {
  const refresh = deferred();
  const { setState, options } = runnerOptions({
    post: async () => actionResponse({ status: "approval_pending" }),
    onRefreshActivity: () => refresh.promise.then(() => Promise.reject(new Error("Retired refresh"))),
  });
  await runGuardedConnectorAction(options);
  options.requestGuard.setScope("target:2");
  setState.mockClear();
  refresh.resolve(undefined);
  await refresh.promise;
  await new Promise<void>((resolve) => setTimeout(resolve, 0));
  expect(setState).not.toHaveBeenCalled();
});

it("drops a completed result whose scope is retired during activity refresh", async () => {
  const refresh = deferred();
  const onRefreshActivity = vi.fn(() => refresh.promise);
  const { setState, options } = runnerOptions({ post: async () => actionResponse(), onRefreshActivity });
  const running = runGuardedConnectorAction(options);
  await vi.waitFor(() => expect(onRefreshActivity).toHaveBeenCalledOnce());
  options.requestGuard.setScope("target:2");
  setState.mockClear();
  refresh.resolve(undefined);
  await expect(running).resolves.toBeNull();
  expect(setState).not.toHaveBeenCalled();
});

it("preserves an action-result uncertain outcome and appends a refresh failure without retrying", async () => {
  const failure = Object.assign(new Error("Lost result"), {
    actionItem: { status: "outcome_unknown", request_id: 12, assistant_hint: "Inspect external state." },
  });
  const post = vi.fn(async () => Promise.reject(failure));
  const { setState, options } = runnerOptions({
    post,
    onRefreshActivity: async () => Promise.reject(new Error("Offline")),
  });
  await expect(runGuardedConnectorAction(options)).rejects.toBe(failure);
  expect(post).toHaveBeenCalledOnce();
  expect(setState).toHaveBeenLastCalledWith({
    state: "error",
    error: "Lost result Request 12. Inspect external state. Activity refresh failed: Offline",
    message: "",
  });
});
