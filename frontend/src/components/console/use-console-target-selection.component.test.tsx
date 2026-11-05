import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useConsoleTargetSelection } from "./use-console-target-selection";

const targets = [
  { ref: "postgres:1:10", connector_kind: "postgres", target_id: 1, profile_id: 10, profile_label: "admin", name: "main-db" },
  { ref: "postgres:1:11", connector_kind: "postgres", target_id: 1, profile_id: 11, profile_label: "readonly", name: "main-db" },
  { ref: "ssh:2:20", connector_kind: "ssh", target_id: 2, profile_id: 20, profile_label: "root", name: "ops" },
];

type SelectionProps = Parameters<typeof useConsoleTargetSelection<(typeof targets)[number]>>[0];

function props(overrides: Partial<Omit<SelectionProps, "setSearchParams">> = {}) {
  return {
    messages: { data: [] as SelectionProps["messages"]["data"] },
    selectedTargetRef: "postgres:1:10",
    setSearchParams: vi.fn(),
    targets: { state: "ready", data: targets, error: null },
    ...overrides,
  };
}

describe("useConsoleTargetSelection", () => {
  it.each(["", "missing"])("leaves URL target %j unselected even when targets and unread messages arrive", (selectedTargetRef) => {
    const value = props({ selectedTargetRef });
    const { result, rerender } = renderHook((next: ReturnType<typeof props>) => useConsoleTargetSelection(next), {
      initialProps: props({ selectedTargetRef, targets: { data: [] } }),
    });
    expect(result.current.selectedTarget).toBeNull();
    rerender(value);
    expect(result.current.selectedTarget).toBeNull();
    expect(result.current.selectedProfiles).toEqual([]);
    expect(result.current.selectedRuntimeID).toBe("");
    rerender({ ...value, messages: { data: [{ direction: "ai_to_user", consumed_at: null, runtime_id: 2 }] } });
    expect(result.current.unreadMessages).toHaveLength(1);
    expect(result.current.selectedTarget).toBeNull();
    expect(value.setSearchParams).not.toHaveBeenCalled();
    act(() => result.current.selectTarget(targets[0]));
    expect(value.setSearchParams).toHaveBeenCalledWith({ target: "postgres:1:10" });
  });

  it.each(["", "missing"])("does not select another target after URL changes to %j", (selectedTargetRef) => {
    const value = props();
    const { result, rerender } = renderHook((next) => useConsoleTargetSelection(next), { initialProps: value });
    expect(result.current.selectedTarget?.ref).toBe(value.selectedTargetRef);
    rerender({ ...value, selectedTargetRef });
    expect(result.current.selectedTarget).toBeNull();
    expect(value.setSearchParams).not.toHaveBeenCalled();
  });

  it("resolves an explicit profile when targets arrive and does not replace it after removal", () => {
    const value = props({ selectedTargetRef: "postgres:1:11" });
    const { result, rerender } = renderHook((next: ReturnType<typeof props>) => useConsoleTargetSelection(next), {
      initialProps: props({ selectedTargetRef: value.selectedTargetRef, targets: { data: [] } }),
    });
    rerender(value);
    expect(result.current.selectedTarget?.profile_id).toBe(11);
    rerender({ ...value, targets: { ...value.targets, data: targets.filter((target) => target.ref !== value.selectedTargetRef) } });
    expect(result.current.selectedTarget).toBeNull();
    expect(value.setSearchParams).not.toHaveBeenCalled();
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
    expect(result.current.selectedTarget?.ref).toBe("postgres:1:10");
    act(() => result.current.toggleProject("3"));
    expect(result.current.collapsedProjects).toEqual({ "3": true });
    act(() => result.current.toggleProject("3"));
    expect(result.current.collapsedProjects).toEqual({ "3": false });
  });
});
