import { connectorActionRequest } from "../../../test/connector-action-fixtures";
import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { MailActionFailure, useMailActionRunner } from "./use-mail-action-runner";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";
import { APIError } from "../../../lib/errors";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

function deferred<T = void>() {
  let resolve: (_value: T) => void = () => { throw new Error("Deferred promise was not initialized."); };
  let reject: (_error: Error) => void = () => { throw new Error("Deferred promise was not initialized."); };
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

beforeEach(() => { vi.mocked(apiPost).mockReset(); });

it("does not let an old activity refresh failure replace a newer target result", async () => {
  const oldRefresh = deferred();
  const refreshes = [() => oldRefresh.promise, () => Promise.resolve()];
  const props = {
    target: { ref: "mail:1:1" },
    approvals: { data: [] },
    scopeKey: "mail:1:1:session-a",
    onRefreshActivity: vi.fn(() => refreshes.shift()?.()),
    onResolution: vi.fn(),
  };
  vi.mocked(apiPost).mockImplementation(async (_path, payload) => connectorActionFixture({ target_ref: connectorActionRequest(payload).target_ref, action_name: connectorActionRequest(payload).action_name, output: {} }));
  const { result, rerender } = renderHook((value) => useMailActionRunner(value), { initialProps: props });

  await act(async () => result.current.runMailAction("send_message", {}, "first send"));
  rerender({ ...props, target: { ref: "mail:2:2" }, scopeKey: "mail:2:2:session-b" });
  await act(async () => result.current.runMailAction("send_message", {}, "second send"));
  await act(async () => oldRefresh.reject(new Error("old refresh failed")));

  expect(result.current.state).toMatchObject({
    state: "idle",
    error: "",
    message: "Message accepted for SMTP delivery.",
  });
});

it("rejects wrong-target and malformed action envelopes before reporting completion", async () => {
  const props = { target: { ref: "mail:1:1" }, approvals: { data: [] }, scopeKey: "mail:1:1:active", onRefreshActivity: vi.fn() };
  const { result } = renderHook(() => useMailActionRunner(props));
  vi.mocked(apiPost).mockResolvedValue(connectorActionFixture({ target_ref: "mail:2:2", action_name: "get_message" }));
  await act(async () => {
    await expect(result.current.runMailAction("get_message", {}, "read")).rejects.toThrow("Invalid connector action response");
  });
  expect(result.current.state).toMatchObject({ state: "error", message: "" });
  expect(props.onRefreshActivity).not.toHaveBeenCalled();
  vi.mocked(apiPost).mockResolvedValue({ status: "completed", output: { subject: "untrusted" } });
  await act(async () => {
    await expect(result.current.runMailAction("get_message", {}, "read")).rejects.toThrow("Invalid connector action response");
  });
  expect(result.current.state.result?.item).toBeNull();
});

it("discards a delayed response after the same target starts a different session", async () => {
  const pending = deferred<ReturnType<typeof connectorActionFixture>>();
  vi.mocked(apiPost).mockReturnValue(pending.promise);
  const props = { target: { ref: "mail:1:1" }, scopeKey: "session-a", onRefreshActivity: vi.fn(), onResolution: vi.fn() };
  const { result, rerender } = renderHook((value) => useMailActionRunner(value), { initialProps: props });
  let request: ReturnType<typeof result.current.runMailAction> | undefined;
  act(() => { request = result.current.runMailAction("get_message", {}, "read"); });
  rerender({ ...props, scopeKey: "session-b" });
  await act(async () => {
    pending.resolve(connectorActionFixture({ target_ref: props.target.ref, action_name: "get_message" }));
    expect(await request).toBeNull();
  });
  expect(result.current.state).toEqual({ state: "idle", error: "", message: "" });
  expect(props.onRefreshActivity).not.toHaveBeenCalled();
  expect(props.onResolution).not.toHaveBeenCalled();
  expect(result.current.resultDialog.open).toBe(false);
});

it("ignores a delayed rejection after unmount without refreshing activity", async () => {
  const pending = deferred<ReturnType<typeof connectorActionFixture>>();
  vi.mocked(apiPost).mockReturnValue(pending.promise);
  const onRefreshActivity = vi.fn();
  const { result, unmount } = renderHook(() => useMailActionRunner({ target: { ref: "mail:1:1" }, scopeKey: "session-a", onRefreshActivity }));
  let request: ReturnType<typeof result.current.runMailAction> | undefined;
  act(() => { request = result.current.runMailAction("get_message", {}, "read"); });
  unmount();
  await act(async () => { pending.reject(new Error("delayed failure")); expect(await request).toBeNull(); });
  expect(onRefreshActivity).not.toHaveBeenCalled();
});

it("preserves unknown SMTP metadata and the original transport error identity", async () => {
  const response = connectorActionFixture({ target_ref: "mail:1:1", action_name: "send_message", status: "outcome_unknown", error: "SMTP result unknown", output: { submission_status: "submission_unknown", message_id: "test-message" } });
  vi.mocked(apiPost).mockResolvedValue(response);
  const { result } = renderHook(() => useMailActionRunner({ target: { ref: "mail:1:1" }, scopeKey: "session-a" }));
  await act(async () => {
    await expect(result.current.runMailAction("send_message", {}, "send")).rejects.toMatchObject({ actionItem: response, actionResult: { item: response } });
  });
  expect(result.current.resultDialog).toMatchObject({ open: true, item: response });
  expect(result.current.state).toMatchObject({ state: "error", error: "SMTP result unknown" });
  const failure = new APIError("Transport rejected", { status: 409, code: "test-error", data: { detail: "preserved" } });
  vi.mocked(apiPost).mockRejectedValue(failure);
  await act(async () => { await expect(result.current.runMailAction("send_message", {}, "send")).rejects.toBe(failure); });
  expect(failure).not.toBeInstanceOf(MailActionFailure);
  expect(failure).toMatchObject({ status: 409, code: "test-error", data: { detail: "preserved" } });
});
