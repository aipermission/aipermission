import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  groupActions,
  groupActionsByRisk,
  inferPermissionMode,
  matchesPermissionMutationError,
  ruleForActions,
  tokenProfileModeKey,
} from "../../components/console/connector-token-permission-model";
import type { TokenActionPermission } from "../../lib/gateway-contracts/security-contracts";

const target = { connector_kind: "example", target_id: 3 };
const actions = [
  { name: "list", risk: "read", category: "browse" },
  { name: "inspect", risk: "read", category: "browse" },
  { name: "update", risk: "write", category: "edit" },
  { name: "remove", risk: "destructive" },
];
type Permission = Pick<TokenActionPermission, "target_id" | "profile_id" | "action_name" | "execution_rule" | "expires_at">;

function permission(action_name: string, execution_rule: Permission["execution_rule"], overrides: Partial<Permission> = {}): Permission {
  return { target_id: 3, profile_id: 7, action_name, execution_rule, expires_at: "", ...overrides };
}

beforeEach(() => vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-09-27T00:00:00Z")));
afterEach(() => vi.restoreAllMocks());

it("classifies uniform, risk-grouped and genuinely mixed action rules", () => {
  const basic = actions.map((action) => permission(action.name, "always_run"));
  const grouped = basic.map<Permission>((item) => ({ ...item, execution_rule: item.action_name === "update" ? "blocked" : "always_run" }));
  const advanced = grouped.map<Permission>((item) => ({
    ...item,
    execution_rule: item.action_name === "inspect" ? "approval_required" : item.execution_rule,
  }));
  expect(inferPermissionMode(basic, target, 7, actions)).toBe("basic");
  expect(ruleForActions(basic, target, 7, actions)).toBe("always_run");
  expect(inferPermissionMode(grouped, target, 7, actions)).toBe("grouped");
  expect(ruleForActions(grouped, target, 7, actions)).toBe("mixed");
  expect(inferPermissionMode(advanced, target, 7, actions)).toBe("advanced");
});

it("never borrows rules from other targets, profiles, actions or expired grants", () => {
  const rows = [
    permission("list", "always_run", { target_id: 4 }),
    permission("list", "always_run", { profile_id: 8 }),
    permission("unrelated", "always_run"),
    permission("list", "always_run", { expires_at: "2026-09-26T00:00:00Z" }),
    permission("inspect", "approval_required"),
  ];
  expect(ruleForActions(rows, target, 7, [actions[0]])).toBe("");
  expect(ruleForActions(rows, target, 7, actions.slice(0, 2))).toBe("mixed");
  expect(inferPermissionMode(rows, target, 7, actions)).toBe("advanced");
  expect(ruleForActions(rows, null, 7, actions)).toBe("");
  expect(ruleForActions(rows, target, 7, [])).toBe("");
  expect(inferPermissionMode(rows, undefined, 7, actions)).toBe("basic");
  expect(inferPermissionMode(rows, target, 7, [])).toBe("basic");
  expect(inferPermissionMode([], target, 7, actions)).toBe("basic");
});

it("groups native action metadata without dropping uncategorized actions or changing category order", () => {
  expect(groupActions(actions)).toEqual([
    { name: "browse", actions: actions.slice(0, 2) },
    { name: "edit", actions: [actions[2]] },
    { name: "actions", actions: [actions[3]] },
  ]);
  expect(groupActions([])).toEqual([]);
  const unknown = { name: "custom", risk: "custom-risk" };
  const risks = groupActionsByRisk([...actions, unknown]);
  expect(risks.map((group) => group.key)).toEqual(["read", "write", "destructive", "credential_sensitive", "other"]);
  expect(risks[0]).toMatchObject({ name: "Read operations", description: "2 read-only actions", actions: actions.slice(0, 2) });
  expect(risks[3]).toMatchObject({ description: "No credential-sensitive actions exposed.", actions: [] });
  expect(risks[4]).toMatchObject({ actions: [unknown], description: "1 uncategorized action" });
});

it("scopes saved modes and mutation errors to the exact token, target and profile", () => {
  expect(tokenProfileModeKey(2, target, 7)).toBe("2:example:3:7");
  expect(tokenProfileModeKey(2, null, "")).toBe("2:::");
  expect(matchesPermissionMutationError({ targetKey: "example:3", tokenID: 2, profileID: 7 }, 2, 7, "example:3")).toBe(true);
  expect(matchesPermissionMutationError({ targetKey: "example:3", tokenID: 2, profileID: 8 }, 2, 7, "example:3")).toBe(false);
  expect(matchesPermissionMutationError({ targetKey: "example:3", tokenID: 3, profileID: 7 }, 2, 7, "example:3")).toBe(false);
  expect(matchesPermissionMutationError({ targetKey: "example:4", tokenID: 2, profileID: 7 }, 2, 7, "example:3")).toBe(false);
  expect(matchesPermissionMutationError(null, 2, 7, "example:3")).toBe(false);
});
