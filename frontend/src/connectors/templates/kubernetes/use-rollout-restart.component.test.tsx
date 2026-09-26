import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import { useRolloutRestart, type RolloutRestartProps } from "./use-rollout-restart";

function deferred() {
  let resolve!: (_item: ConnectorActionResponse | null) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<ConnectorActionResponse | null>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

const first = { kind: "Deployment", namespace: "apps", name: "api-a" };
const second = { kind: "Deployment", namespace: "apps", name: "api-b" };
const completed: ConnectorActionResponse = {
  request_id: 1,
  status: "completed",
  target_ref: "kubernetes:2:2",
  connector_kind: "kubernetes",
  action_name: "rollout_restart",
  retry_policy: { class: "non_idempotent", guidance: "Inspect the deployment before retrying." },
  output: {},
};

function options(overrides: Partial<RolloutRestartProps> = {}): RolloutRestartProps {
  return {
    targetRef: "kubernetes:1:1",
    tab: "workloads",
    selectedResource: first,
    runAction: vi.fn().mockResolvedValue(null),
    refreshResource: vi.fn().mockResolvedValue(undefined),
    ...overrides,
  };
}

it.each(["completed", "pending", "failed"])("does not let an old %s restart alter the new target's dialog", async (outcome) => {
  const oldRequest = deferred();
  const newRequest = deferred();
  const runAction = vi
    .fn<RolloutRestartProps["runAction"]>()
    .mockReturnValueOnce(oldRequest.promise)
    .mockReturnValueOnce(newRequest.promise);
  const props = options({ runAction });
  const { result, rerender } = renderHook((value) => useRolloutRestart(value), { initialProps: props });
  act(() => result.current.open());
  let oldConfirmation!: Promise<void>;
  act(() => {
    oldConfirmation = result.current.confirm();
  });
  rerender({ ...props, targetRef: "kubernetes:2:2", selectedResource: second });
  act(() => result.current.open());
  let newConfirmation!: Promise<void>;
  act(() => {
    newConfirmation = result.current.confirm();
  });
  await act(async () => {
    if (outcome === "failed") oldRequest.reject(new Error("Retired restart failed"));
    else oldRequest.resolve(outcome === "completed" ? { ...completed, target_ref: "kubernetes:1:1" } : null);
    await oldConfirmation;
  });
  expect(result.current.dialog).toEqual({ open: true, pending: true, workload: second });
  expect(props.refreshResource).not.toHaveBeenCalled();
  await act(async () => {
    newRequest.resolve(completed);
    await newConfirmation;
  });
  expect(result.current.dialog).toEqual({ open: false, pending: false, workload: null });
  expect(props.refreshResource).toHaveBeenCalledExactlyOnceWith("workloads");
});

it("does not dispatch duplicate confirmations before the pending state rerenders", async () => {
  const pending = deferred();
  const props = options({ runAction: vi.fn().mockReturnValue(pending.promise) });
  const { result } = renderHook(() => useRolloutRestart(props));
  act(() => result.current.open());
  let firstConfirmation!: Promise<void>;
  let duplicate!: Promise<void>;
  act(() => {
    firstConfirmation = result.current.confirm();
    duplicate = result.current.confirm();
  });
  expect(props.runAction).toHaveBeenCalledOnce();
  act(() => {
    result.current.close();
    result.current.open(second);
  });
  expect(result.current.dialog).toEqual({ open: true, pending: true, workload: first });
  await act(async () => {
    pending.resolve(null);
    await Promise.all([firstConfirmation, duplicate]);
  });
  expect(result.current.dialog).toEqual({ open: true, pending: false, workload: first });
  expect(props.refreshResource).not.toHaveBeenCalled();
});

it("allows cancel before submission and refuses unsupported or empty restart targets", async () => {
  const props = options();
  const { result, rerender } = renderHook((value) => useRolloutRestart(value), { initialProps: props });
  await act(async () => result.current.confirm());
  act(() => result.current.open());
  expect(result.current.dialog.open).toBe(true);
  act(() => result.current.close());
  expect(result.current.dialog).toEqual({ open: false, pending: false, workload: null });
  rerender({ ...props, tab: "pods" });
  act(() => result.current.open());
  expect(result.current.dialog.open).toBe(false);
  rerender({ ...props, selectedResource: { ...first, kind: "StatefulSet" } });
  act(() => result.current.open());
  expect(result.current.dialog.open).toBe(false);
  rerender({ ...props, selectedResource: null });
  act(() => result.current.open());
  expect(result.current.dialog.open).toBe(false);
  expect(props.runAction).not.toHaveBeenCalled();
});

it("releases a failed current attempt so the captured deployment remains retryable", async () => {
  const runAction = vi
    .fn<RolloutRestartProps["runAction"]>()
    .mockRejectedValueOnce(new Error("Restart refused"))
    .mockResolvedValueOnce(null);
  const props = options({ runAction });
  const { result } = renderHook(() => useRolloutRestart(props));
  act(() => result.current.open());
  await act(async () => {
    await expect(result.current.confirm()).rejects.toThrow("Restart refused");
  });
  expect(result.current.dialog).toEqual({ open: true, pending: false, workload: first });
  await act(async () => result.current.confirm());
  expect(runAction).toHaveBeenCalledTimes(2);
  expect(props.refreshResource).not.toHaveBeenCalled();
});

it("does not refresh resources when a completed restart belongs to an unmounted dialog", async () => {
  const pending = deferred();
  const props = options({ runAction: vi.fn().mockReturnValue(pending.promise) });
  const { result, unmount } = renderHook(() => useRolloutRestart(props));
  act(() => result.current.open());
  let confirmation!: Promise<void>;
  act(() => {
    confirmation = result.current.confirm();
  });
  unmount();
  await act(async () => {
    pending.resolve(completed);
    await confirmation;
  });
  expect(props.refreshResource).not.toHaveBeenCalled();
});
