import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useConsoleTargetSelection } from "./use-console-target-selection";

const targets = [
  { ref: "postgres:1:10", connector_kind: "postgres", target_id: 1, profile_id: 10, profile_label: "admin", name: "main-db" },
  { ref: "postgres:1:11", connector_kind: "postgres", target_id: 1, profile_id: 11, profile_label: "readonly", name: "main-db" },
  { ref: "ssh:2:20", connector_kind: "ssh", target_id: 2, profile_id: 20, profile_label: "root", name: "ops" },
];

function props(overrides = {}) {
  return {
    messages: { data: [] },
    pendingApprovals: [],
    selectedTargetRef: "postgres:1:10",
    setSearchParams: vi.fn(),
    targets: { state: "ready", data: targets, error: null },
    ...overrides,
  };
}

describe("useConsoleTargetSelection", () => {
  it("replaces an invalid URL target with the deterministic fallback", () => {
    const value = props({ selectedTargetRef: "missing" });
    renderHook(() => useConsoleTargetSelection(value));

    expect(value.setSearchParams).toHaveBeenCalledWith({ target: "postgres:1:10" }, { replace: true });
  });

  it("remembers the selected profile when the target is selected again", () => {
    const value = props();
    const { result, rerender } = renderHook((next) => useConsoleTargetSelection(next), { initialProps: value });

    act(() => result.current.selectProfile(11));
    rerender({ ...value, selectedTargetRef: "postgres:1:11" });
    value.setSearchParams.mockClear();
    act(() => result.current.selectTarget(targets[0]));

    expect(value.setSearchParams).toHaveBeenCalledWith({ target: "postgres:1:11" });
  });

  it("filters targets by profile labels without changing the selected target", () => {
    const { result } = renderHook(() => useConsoleTargetSelection(props()));

    act(() => result.current.setSearch("readonly"));

    expect(result.current.filteredTargets.map((target) => target.target_id)).toEqual([1]);
    expect(result.current.selectedTarget.ref).toBe("postgres:1:10");
  });
});
