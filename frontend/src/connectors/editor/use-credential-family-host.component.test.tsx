import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useCredentialFamilyHost } from "./use-credential-family-host";
import { idleActionState } from "../../lib/use-async-action";

it("closes other native drafts but never its own when switching active families", () => {
  const { result } = renderHook(() => useCredentialFamilyHost());
  const first = { openCreate: vi.fn(), close: vi.fn() };
  const second = { openCreate: vi.fn(), close: vi.fn() };
  act(() => {
    result.current.register("first", first);
    result.current.register("second", second);
    result.current.onOpen("second");
  });
  expect(first.close).toHaveBeenCalledOnce();
  expect(second.close).not.toHaveBeenCalled();
  act(() => result.current.openCreate("second"));
  expect(second.openCreate).toHaveBeenCalledOnce();
  act(() => result.current.openCreate("missing"));
  expect(first.openCreate).not.toHaveBeenCalled();
});

it("ignores retired family state and clears an orphaned pending action on unregister", () => {
  const { result } = renderHook(() => useCredentialFamilyHost());
  act(() => {
    result.current.register("active", { openCreate: vi.fn(), close: vi.fn() });
    result.current.onOpen("active");
    result.current.onStateChange("active", { state: "saving", error: null, message: null });
  });
  expect(result.current.busy).toBe(true);
  act(() => result.current.onStateChange("retired", { state: "error", error: "Retired error", message: null }));
  expect(result.current.state.state).toBe("saving");
  act(() => result.current.register("active", null));
  expect(result.current.state).toEqual(idleActionState);
  expect(result.current.busy).toBe(false);
  act(() => result.current.onStateChange("active", { state: "error", error: "Stale result", message: null }));
  expect(result.current.state).toEqual(idleActionState);
});

it("tracks row counts without emitting redundant updates and removes retired counts", () => {
  const { result } = renderHook(() => useCredentialFamilyHost());
  act(() => result.current.onRowsChange("first", 3));
  const previous = result.current.rowCounts;
  act(() => result.current.onRowsChange("first", 3));
  expect(result.current.rowCounts).toBe(previous);
  act(() => result.current.onRowsChange("second", 0));
  expect(result.current.rowCounts.get("second")).toBe(0);
  act(() => result.current.onRowsChange("first", null));
  expect(result.current.rowCounts.has("first")).toBe(false);
  const next = result.current.rowCounts;
  act(() => result.current.onRowsChange("first", null));
  expect(result.current.rowCounts).toBe(next);
});
