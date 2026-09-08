import { act, renderHook } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { useMailActionRunner } from "./use-mail-action-runner";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn() }));

function deferred() {
  let reject;
  const promise = new Promise((_resolve, rejectPromise) => {
    reject = rejectPromise;
  });
  return { promise, reject };
}

beforeEach(() => apiPost.mockReset());

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
  apiPost.mockResolvedValue({ id: 1, status: "completed", action_name: "send_message", output: {} });
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
