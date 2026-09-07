import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useConsoleConnectorView } from "./use-console-connector-view";

const Console = () => null;
const ToolbarActions = () => null;
const Operations = () => null;

function renderView(selectedTarget = { connector_kind: "example" }) {
  const resolveTemplate = vi.fn(() => ({ Console, Operations, ToolbarActions }));
  return { resolveTemplate, ...renderHook(() => useConsoleConnectorView({ resolveTemplate, selectedTarget })) };
}

describe("useConsoleConnectorView", () => {
  it("resolves connector-owned slots without exposing connector-kind branches", () => {
    const { resolveTemplate, result } = renderView();

    expect(resolveTemplate).toHaveBeenCalledWith("example");
    expect(result.current.Console).toBe(Console);
    expect(result.current.ToolbarActions).toBe(ToolbarActions);
    expect(result.current.OperationTemplate).toBeNull();

    act(() => {
      expect(result.current.openOperation({ open: true, connector_kind: "example", type: "manage" })).toBe(true);
    });

    expect(result.current.OperationTemplate).toBe(Operations);
    expect(result.current.operation.type).toBe("manage");
  });

  it("rejects malformed operation requests and owns activity visibility", () => {
    const { result } = renderView();

    act(() => {
      expect(result.current.openOperation({ open: false, connector_kind: "example" })).toBe(false);
      expect(result.current.openOperation({ open: true })).toBe(false);
      result.current.openActivity();
    });
    expect(result.current.operation.open).toBe(false);
    expect(result.current.activityOpen).toBe(true);

    act(() => result.current.closeActivity());
    expect(result.current.activityOpen).toBe(false);
  });
});
