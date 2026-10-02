import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useS3ObjectDelete } from "./use-s3-object-delete";

function deferred<T>() {
  let resolve!: (_value: T) => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<T>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}

function renderDeletion() {
  const first = deferred<object | null>();
  const second = deferred<object | null>();
  const runAction = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  const clearSelection = vi.fn();
  const refreshObjects = vi.fn().mockResolvedValue(null);
  const initialProps = { scopeKey: "scope-a", selectedKey: "first.txt" };
  const hook = renderHook((props) => useS3ObjectDelete({ ...props, runAction, clearSelection, refreshObjects }), { initialProps });
  return { ...hook, first, second, runAction, clearSelection, refreshObjects, initialProps };
}

it.each(["completed", "pending", "rejected"])("does not settle a replacement confirmation after a retired %s delete", async (outcome) => {
  const hook = renderDeletion();
  act(() => hook.result.current.requestDelete());
  let oldAttempt!: Promise<void>;
  act(() => {
    oldAttempt = hook.result.current.confirmPendingAction();
  });
  hook.rerender({ scopeKey: "scope-b", selectedKey: "second.txt" });
  act(() => hook.result.current.requestDelete());
  let newAttempt!: Promise<void>;
  act(() => {
    newAttempt = hook.result.current.confirmPendingAction();
  });
  await act(async () => {
    if (outcome === "rejected") hook.first.reject(new Error("old-delete-error"));
    else hook.first.resolve(outcome === "completed" ? {} : null);
    await oldAttempt;
  });
  expect(hook.result.current.confirmDialog).toMatchObject({ open: true, pending: true, error: "", status: "" });
  expect(hook.result.current.confirmDialog.details).toEqual([{ label: "Object", value: '"second.txt"' }]);
  expect(hook.clearSelection).not.toHaveBeenCalled();
  expect(hook.refreshObjects).not.toHaveBeenCalled();
  await act(async () => {
    hook.second.resolve(null);
    await newAttempt;
  });
  expect(hook.result.current.confirmDialog).toMatchObject({ open: true, pending: false, status: expect.stringContaining("pending") });
});

it.each(["completed", "rejected"])("does not close or rewrite a replacement after a retired %s refresh", async (outcome) => {
  const hook = renderDeletion();
  const refresh = deferred<null>();
  hook.refreshObjects.mockReturnValueOnce(refresh.promise);
  act(() => hook.result.current.requestDelete());
  let oldAttempt!: Promise<void>;
  act(() => {
    oldAttempt = hook.result.current.confirmPendingAction();
  });
  await act(async () => hook.first.resolve({}));
  expect(hook.refreshObjects).toHaveBeenCalledOnce();
  hook.rerender({ scopeKey: "scope-b", selectedKey: "second.txt" });
  act(() => hook.result.current.requestDelete());
  await act(async () => {
    if (outcome === "rejected") refresh.reject(new Error("old-refresh-error"));
    else refresh.resolve(null);
    await oldAttempt;
  });
  expect(hook.result.current.confirmDialog).toMatchObject({ open: true, pending: false, error: "", status: "" });
  expect(hook.result.current.confirmDialog.details).toEqual([{ label: "Object", value: '"second.txt"' }]);
});

it("admits only one confirmation before a pending state render", async () => {
  const hook = renderDeletion();
  act(() => hook.result.current.requestDelete());
  const confirm = hook.result.current.confirmPendingAction;
  let attempt!: Promise<void>;
  act(() => {
    attempt = confirm();
    void confirm();
  });
  expect(hook.runAction).toHaveBeenCalledOnce();
  await act(async () => {
    hook.first.resolve({});
    await attempt;
  });
  expect(hook.result.current.confirmDialog.open).toBe(false);
});

it("retires delete authority before refresh and never replays a successful mutation after refresh failure", async () => {
  const hook = renderDeletion();
  const refresh = deferred<null>();
  hook.refreshObjects.mockReturnValueOnce(refresh.promise);
  act(() => hook.result.current.requestDelete());
  const retainedConfirm = hook.result.current.confirmPendingAction;
  const retainedAction = hook.result.current.confirmDialog.action!;
  let attempt!: Promise<void>;
  act(() => {
    attempt = retainedConfirm();
  });
  await act(async () => hook.first.resolve({}));
  expect(hook.result.current.confirmDialog).toMatchObject({ action: null, title: "S3 object deleted", pending: true, danger: false });
  await act(async () => hook.result.current.confirmPendingAction());
  expect(hook.runAction).toHaveBeenCalledOnce();
  await act(async () => {
    refresh.reject(new Error("listing reload unavailable"));
    await attempt;
    await retainedConfirm();
    await hook.result.current.confirmPendingAction();
    expect(await retainedAction()).toBe(false);
  });
  expect(hook.runAction).toHaveBeenCalledOnce();
  expect(hook.clearSelection).toHaveBeenCalledOnce();
  expect(hook.refreshObjects).toHaveBeenCalledOnce();
  expect(hook.result.current.confirmDialog).toMatchObject({
    open: true,
    action: null,
    pending: false,
    error: "listing reload unavailable",
    status: expect.stringContaining("Deletion completed"),
  });
  act(() => hook.result.current.closeConfirmDialog());
  expect(hook.result.current.confirmDialog.open).toBe(false);
});

it("pins the newer same-scope dialog and refuses the retained previous confirm callback", async () => {
  const hook = renderDeletion();
  act(() => hook.result.current.requestDelete());
  const oldConfirm = hook.result.current.confirmPendingAction;
  hook.rerender({ scopeKey: "scope-a", selectedKey: "second.txt" });
  act(() => hook.result.current.requestDelete());
  await act(async () => oldConfirm());
  expect(hook.runAction).not.toHaveBeenCalled();
  let attempt!: Promise<void>;
  act(() => {
    attempt = hook.result.current.confirmPendingAction();
  });
  expect(hook.runAction).toHaveBeenCalledWith(expect.objectContaining({ input: { key: "second.txt" } }));
  await act(async () => {
    hook.first.resolve({});
    await attempt;
  });
  expect(hook.result.current.confirmDialog.open).toBe(false);
});

it("does not open without an object or dispatch without a confirmation", async () => {
  const hook = renderDeletion();
  hook.rerender({ scopeKey: "scope-a", selectedKey: "" });
  act(() => hook.result.current.requestDelete());
  await act(async () => hook.result.current.confirmPendingAction());
  expect(hook.result.current.confirmDialog.open).toBe(false);
  expect(hook.runAction).not.toHaveBeenCalled();
});

it("cannot dispatch a retained confirmation after its dialog is canceled", async () => {
  const hook = renderDeletion();
  act(() => hook.result.current.requestDelete());
  const confirm = hook.result.current.confirmPendingAction;
  act(() => hook.result.current.closeConfirmDialog());
  let attempt!: Promise<void>;
  act(() => {
    attempt = confirm();
  });
  await act(async () => {
    hook.first.resolve({});
    await attempt;
  });
  expect(hook.runAction).not.toHaveBeenCalled();
});

it("retires the first generation when the same scope and object are selected again", async () => {
  const hook = renderDeletion();
  act(() => hook.result.current.requestDelete());
  let oldAttempt!: Promise<void>;
  act(() => {
    oldAttempt = hook.result.current.confirmPendingAction();
  });
  hook.rerender({ scopeKey: "scope-b", selectedKey: "second.txt" });
  hook.rerender(hook.initialProps);
  act(() => hook.result.current.requestDelete());
  await act(async () => {
    hook.first.resolve({});
    await oldAttempt;
  });
  expect(hook.result.current.confirmDialog).toMatchObject({ open: true, pending: false });
  expect(hook.clearSelection).not.toHaveBeenCalled();
  expect(hook.refreshObjects).not.toHaveBeenCalled();
});

it("does not refresh, clear selection, or dispatch after unmount", async () => {
  const hook = renderDeletion();
  act(() => hook.result.current.requestDelete());
  const confirm = hook.result.current.confirmPendingAction;
  let attempt!: Promise<void>;
  act(() => {
    attempt = confirm();
  });
  hook.unmount();
  await act(async () => {
    hook.first.resolve({});
    await attempt;
    await confirm();
  });
  expect(hook.runAction).toHaveBeenCalledOnce();
  expect(hook.clearSelection).not.toHaveBeenCalled();
  expect(hook.refreshObjects).not.toHaveBeenCalled();
});
