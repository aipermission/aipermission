import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { localActionReconciliationEvent } from "../lib/local-action-retry";
import { useLocalActionReconciliation } from "./use-local-action-reconciliation";

describe("useLocalActionReconciliation", () => {
  it("rejects an earlier request when a new one arrives, then resolves the selected choice", () => {
    const first = vi.fn();
    const second = vi.fn();
    const { result } = renderHook(useLocalActionReconciliation);

    act(() => {
      window.dispatchEvent(new CustomEvent(localActionReconciliationEvent, { detail: { requestID: 1, resolve: first } }));
      window.dispatchEvent(new CustomEvent(localActionReconciliationEvent, { detail: { requestID: 2, resolve: second } }));
    });
    expect(first).toHaveBeenCalledWith(false);
    expect(result.current[0]?.requestID).toBe(2);

    act(() => result.current[1](true));
    expect(second).toHaveBeenCalledWith(true);
    expect(result.current[0]).toBeNull();
  });

  it("keeps a pending external action protected when the UI unmounts", () => {
    const resolve = vi.fn();
    const { unmount } = renderHook(useLocalActionReconciliation);
    act(() => {
      window.dispatchEvent(new CustomEvent(localActionReconciliationEvent, { detail: { resolve } }));
    });
    unmount();
    expect(resolve).toHaveBeenCalledWith(false);
  });
});
