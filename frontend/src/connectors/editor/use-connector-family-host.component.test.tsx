import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { idleActionState } from "../../lib/use-async-action";
import { useConnectorFamilyHost } from "./use-connector-family-host";
import type { ConnectorTestState } from "./use-connector-connection-tests";

function commands() {
  return { openCreate: vi.fn(), openEdit: vi.fn(), requestDelete: vi.fn(), test: vi.fn(async () => true), close: vi.fn() };
}

it("dispatches native inventory commands without losing project or selected profile identity", async () => {
  const { result } = renderHook(() => useConnectorFamilyHost());
  const native = commands();
  const unrelated = commands();
  const target = inventoryTargetFixture({ connector_kind: "future" });
  const profile = inventoryProfileFixture({ connector_kind: "future" });
  act(() => {
    result.current.register("future", native);
    result.current.register("other", unrelated);
    result.current.openCreate("future", "8");
    result.current.openEdit(target, profile);
    result.current.requestDelete(target);
  });
  expect(native.openCreate).toHaveBeenCalledExactlyOnceWith("8");
  expect(native.openEdit).toHaveBeenCalledExactlyOnceWith(target, profile);
  expect(native.requestDelete).toHaveBeenCalledExactlyOnceWith(target);
  await act(async () => {
    expect(await result.current.test(target, profile)).toBe(true);
  });
  expect(native.test).toHaveBeenCalledExactlyOnceWith(target, profile);
  expect(unrelated.openCreate).not.toHaveBeenCalled();
});

it("switches draft ownership and ignores pending/error state from retired families", () => {
  const { result } = renderHook(() => useConnectorFamilyHost());
  const first = commands();
  const second = commands();
  act(() => {
    result.current.register("first", first);
    result.current.register("second", second);
    result.current.onOpen("first");
    result.current.onStateChange("first", { state: "saving", error: null, message: null });
  });
  expect(result.current.busy).toBe(true);
  act(() => result.current.onOpen("second"));
  expect(first.close).toHaveBeenCalledOnce();
  expect(result.current.state).toEqual(idleActionState);
  act(() => result.current.onStateChange("first", { state: "error", error: "Retired error", message: null }));
  expect(result.current.state).toEqual(idleActionState);
  act(() => {
    result.current.onStateChange("second", { state: "saving", error: null, message: null });
    result.current.register("second", null);
  });
  expect(result.current.state).toEqual(idleActionState);
  act(() => result.current.onStateChange("second", { state: "error", error: "Unmounted error", message: null }));
  expect(result.current.busy).toBe(false);
});

it("keeps each family's profile test results until that owner retires", () => {
  const { result } = renderHook(() => useConnectorFamilyHost());
  const first: Record<string, ConnectorTestState> = { "first:1:2": { state: "ok", error: null, data: { ok: true } } };
  const second: Record<string, ConnectorTestState> = { "second:3:4": { state: "error", error: "Connection failed", data: null } };
  act(() => {
    result.current.onTestsChange("first", first);
    result.current.onTestsChange("second", second);
  });
  expect(result.current.tests).toEqual({ ...first, ...second });
  const stable = result.current.tests;
  act(() => result.current.onTestsChange("first", first));
  expect(result.current.tests).toBe(stable);
  act(() => result.current.onTestsChange("first", null));
  expect(result.current.tests).toEqual(second);
  const retired = result.current.tests;
  act(() => result.current.onTestsChange("first", null));
  expect(result.current.tests).toBe(retired);
});

it("does not execute commands for unavailable families or retain retired commands", () => {
  const { result } = renderHook(() => useConnectorFamilyHost());
  const native = commands();
  const target = inventoryTargetFixture({ connector_kind: "future" });
  act(() => {
    result.current.register("future", native);
    result.current.register("future", null);
    result.current.openCreate("future");
    result.current.openEdit(target, null);
    result.current.requestDelete(target);
  });
  expect(result.current.test(target, null)).toBeUndefined();
  for (const action of [native.openCreate, native.openEdit, native.requestDelete, native.test]) expect(action).not.toHaveBeenCalled();
});
