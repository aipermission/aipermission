import { renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useConsolePermissionView } from "./use-console-permission-view";

const now = Date.parse("2026-08-01T12:00:00Z");
const profiles = [
  { target_id: 4, profile_id: 7 },
  { target_id: 4, profile_id: 8 },
];
const target = { connector_kind: "connector", target_id: 4, profile_id: 8 };
const tokens = [
  { id: 1, name: "active" },
  { id: 2, name: "revoked", revoked_at: "2026-07-01T00:00:00Z" },
  { id: 3, name: "expired" },
  { id: 4, name: "project-disabled" },
];

function permission(tokenID, overrides = {}) {
  return {
    token_id: tokenID,
    target_id: 4,
    profile_id: 8,
    action_name: "read",
    execution_rule: "always_run",
    project_enabled: true,
    ...overrides,
  };
}

describe("useConsolePermissionView", () => {
  it("keeps effective permissions bound to the selected target profile", () => {
    const connectorPermissions = {
      1: [permission(1, { profile_id: 7, action_name: "wrong-profile" }), permission(1, { execution_rule: "approval_required" })],
      2: [permission(2)],
      3: [permission(3, { expires_at: "2026-08-01T11:00:00Z" })],
      4: [permission(4, { project_enabled: false })],
    };
    const { result } = renderHook(() =>
      useConsolePermissionView({ connectorPermissions, mcpEnabled: true, now, profiles, target, tokens }),
    );

    expect(result.current.selectedTokenOptions.map((token) => token.id)).toEqual([1]);
    expect(result.current.alwaysRunTokenPermissions).toEqual([]);
    expect(result.current.showAlwaysRunWarning).toBe(false);
  });

  it("reports temporary Always access only while MCP execution is enabled", () => {
    const connectorPermissions = {
      1: [permission(1, { expires_at: "2026-08-01T14:00:00Z" })],
    };
    const { result, rerender } = renderHook(
      ({ mcpEnabled }) => useConsolePermissionView({ connectorPermissions, mcpEnabled, now, profiles, target, tokens: [tokens[0]] }),
      { initialProps: { mcpEnabled: false } },
    );

    expect(result.current.alwaysRunTokenPermissions).toHaveLength(1);
    expect(result.current.temporaryAlwaysRunLabels).toEqual(["2h left"]);
    expect(result.current.showAlwaysRunWarning).toBe(false);

    rerender({ mcpEnabled: true });
    expect(result.current.showAlwaysRunWarning).toBe(true);
  });

  it("returns a stable empty view when no target is selected", () => {
    const { result, rerender } = renderHook(
      ({ nextTokens }) =>
        useConsolePermissionView({ connectorPermissions: {}, mcpEnabled: true, now, profiles: [], target: null, tokens: nextTokens }),
      { initialProps: { nextTokens: [] } },
    );
    const first = result.current;

    rerender({ nextTokens: [{ id: 9 }] });

    expect(result.current).toBe(first);
    expect(result.current.selectedTokenOptions).toEqual([]);
  });
});
