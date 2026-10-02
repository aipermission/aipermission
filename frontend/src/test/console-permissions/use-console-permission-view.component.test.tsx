import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { deriveConsolePermissionView, useConsolePermissionView } from "../../components/console/use-console-permission-view";
import type { TokenActionPermission } from "../../lib/gateway-contracts/security-contracts.ts";
import { eligibilityTokens, tokenEligibilityBoundary, tokenEligibilityNow } from "./token-eligibility-fixtures";

const now = tokenEligibilityNow;
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

type Permission = Pick<
  TokenActionPermission,
  "target_id" | "profile_id" | "action_name" | "execution_rule" | "expires_at" | "project_enabled"
> & { token_id: number };
function permission(tokenID: number, overrides: Partial<Permission> = {}): Permission {
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
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(now);
  });
  afterEach(() => vi.useRealTimers());

  it("excludes expired, invalid and revoked tokens even with permanent Always grants", () => {
    const connectorPermissions = Object.fromEntries(eligibilityTokens.map((token) => [token.id, [permission(token.id)]]));
    const { result } = renderHook(() =>
      useConsolePermissionView({ connectorPermissions, mcpEnabled: true, now, profiles, target, tokens: eligibilityTokens }),
    );

    expect(result.current.selectedTokenOptions.map((token) => token.id)).toEqual([5, 6]);
    expect(result.current.alwaysRunTokenPermissions.map(({ token }) => token.id)).toEqual([5, 6]);
    expect(result.current.showAlwaysRunWarning).toBe(true);
    expect(result.current.temporaryAlwaysRunLabels).toEqual([]);
  });

  it("removes the last Always token and warning on live expiry without a grant reload", () => {
    const connectorPermissions = { 6: [permission(6)] };
    const tokenItems = [eligibilityTokens[1]];
    const { result, rerender } = renderHook(() =>
      useConsolePermissionView({ connectorPermissions, mcpEnabled: true, now, profiles, target, tokens: tokenItems }),
    );
    expect(result.current.selectedTokenOptions).toHaveLength(1);
    expect(result.current.showAlwaysRunWarning).toBe(true);

    const boundary = Date.parse(tokenEligibilityBoundary);
    const exactView = deriveConsolePermissionView({
      connectorPermissions,
      mcpEnabled: true,
      now: boundary,
      profiles,
      target,
      tokens: tokenItems,
    });
    expect(exactView.selectedTokenOptions).toEqual([]);
    expect(exactView.alwaysRunTokenPermissions).toEqual([]);
    expect(exactView.showAlwaysRunWarning).toBe(false);

    act(() => vi.advanceTimersByTime(1001));
    expect(result.current.selectedTokenOptions).toEqual([]);
    expect(result.current.alwaysRunTokenPermissions).toEqual([]);
    expect(result.current.showAlwaysRunWarning).toBe(false);

    vi.setSystemTime(boundary);
    rerender();
    expect(result.current.selectedTokenOptions).toEqual([]);
  });

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

  it("ignores project-disabled Always actions when another action keeps the token visible", () => {
    const connectorPermissions = {
      1: [
        permission(1, { action_name: "write", project_enabled: false }),
        permission(1, { action_name: "read", execution_rule: "approval_required" }),
      ],
    };
    const { result } = renderHook(() =>
      useConsolePermissionView({ connectorPermissions, mcpEnabled: true, now, profiles, target, tokens: [tokens[0]] }),
    );

    expect(result.current.selectedTokenOptions).toHaveLength(1);
    expect(result.current.alwaysRunTokenPermissions).toEqual([]);
    expect(result.current.showAlwaysRunWarning).toBe(false);
  });

  it("returns a stable empty view when no target is selected", () => {
    const { result, rerender } = renderHook(
      ({ nextTokens }) =>
        useConsolePermissionView({ connectorPermissions: {}, mcpEnabled: true, now, profiles: [], target: null, tokens: nextTokens }),
      { initialProps: { nextTokens: [] as { id: number; name: string }[] } },
    );
    const first = result.current;

    rerender({ nextTokens: [{ id: 9, name: "Agent" }] });

    expect(result.current).toBe(first);
    expect(result.current.selectedTokenOptions).toEqual([]);
  });
});
