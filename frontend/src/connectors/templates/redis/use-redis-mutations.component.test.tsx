import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { connectorActionFixture } from "../../../test/connector-action-fixtures";
import { useRedisMutations } from "./use-redis-mutations";
import type { RedisMutationOptions } from "./browser-types";

function options(overrides: Partial<RedisMutationOptions> = {}): RedisMutationOptions {
  return {
    resetKey: "redis:1:1:session",
    mutationLocked: false,
    product: "Redis",
    activeKey: "selected",
    keyResult: { key: "selected", type: "string", value: "value" },
    valueDraft: "replacement",
    newKey: "new-key",
    newValue: "new-value",
    ttlDraft: "30",
    selectedKeys: [],
    setState: vi.fn(),
    setNewKey: vi.fn(),
    setNewValue: vi.fn(),
    setKeys: vi.fn(),
    setSelectedKeys: vi.fn(),
    setActiveKey: vi.fn(),
    setKeyResult: vi.fn(),
    setValueDraft: vi.fn(),
    runAction: vi.fn(async () => connectorActionFixture()),
    loadKey: vi.fn(async () => {}),
    ...overrides,
  };
}

it("leaves a failed confirmation editable and retries only after another confirmation", async () => {
  const failure = new Error("synthetic write failure");
  const runAction = vi
    .fn<RedisMutationOptions["runAction"]>()
    .mockRejectedValueOnce(failure)
    .mockResolvedValueOnce(connectorActionFixture());
  const input = options({ runAction });
  const { result } = renderHook(() => useRedisMutations(input));
  act(() => result.current.saveStringValue());
  await act(async () => result.current.confirmPendingAction());
  expect(result.current.confirmDialog).toMatchObject({ open: true, pending: false, error: failure.message });
  expect(input.loadKey).not.toHaveBeenCalled();
  await act(async () => result.current.confirmPendingAction());
  expect(result.current.confirmDialog.open).toBe(false);
  expect(runAction).toHaveBeenCalledTimes(2);
  expect(input.loadKey).toHaveBeenCalledExactlyOnceWith("selected");
});

it.each(["", "invalid", "30"])("normalizes a confirmed TTL %j and reloads the captured key", async (ttlDraft) => {
  const input = options({ ttlDraft });
  const { result } = renderHook(() => useRedisMutations(input));
  act(() => result.current.updateTTL());
  await act(async () => result.current.confirmPendingAction());
  expect(input.runAction).toHaveBeenCalledWith(
    expect.objectContaining({ input: { key: "selected", ttl_seconds: ttlDraft === "30" ? 30 : -1 } }),
  );
  expect(input.loadKey).toHaveBeenCalledExactlyOnceWith("selected");
  expect(result.current.confirmDialog.open).toBe(false);
});

it("removes only confirmed keys and clears the selected key's stale preview", async () => {
  const selectedKeys = ["selected", ...Array.from({ length: 8 }, (_, index) => `key-${index}`)];
  const input = options({ selectedKeys });
  const { result } = renderHook(() => useRedisMutations(input));
  act(() => result.current.deleteSelected());
  expect(result.current.confirmDialog.details.at(-1)).toEqual({ label: "More", value: "1 additional key(s)" });
  await act(async () => result.current.confirmPendingAction());
  expect(input.runAction).toHaveBeenCalledWith(expect.objectContaining({ input: { keys: selectedKeys } }));
  const update = vi.mocked(input.setKeys).mock.calls[0][0];
  expect(typeof update === "function" ? update(["selected", "key-0", "keep"]) : update).toEqual(["keep"]);
  expect(input.setSelectedKeys).toHaveBeenCalledWith([]);
  expect(input.setActiveKey).toHaveBeenCalledWith("");
  expect(input.setKeyResult).toHaveBeenCalledWith(null);
  expect(input.setValueDraft).toHaveBeenCalledWith("");
});

it("retains an unresolved confirmation without claiming deletion success", async () => {
  const input = options({ runAction: vi.fn(async () => null) });
  const { result } = renderHook(() => useRedisMutations(input));
  act(() => result.current.deleteSelected());
  await act(async () => result.current.confirmPendingAction());
  expect(result.current.confirmDialog).toMatchObject({ open: true, pending: false });
  expect(input.setKeys).not.toHaveBeenCalled();
  expect(input.setKeyResult).not.toHaveBeenCalled();
});

it("does not dispatch a locked mutation even if its confirmation is already open", async () => {
  const input = options();
  const { result, rerender } = renderHook((next) => useRedisMutations(next), { initialProps: input });
  act(() => result.current.saveStringValue());
  rerender({ ...input, mutationLocked: true });
  await act(async () => result.current.confirmPendingAction());
  expect(result.current.confirmDialog.pending).toBe(true);
  expect(input.runAction).not.toHaveBeenCalled();
});
