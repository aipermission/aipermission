import { describe, expect, it } from "vitest";
import { groupActions, groupActionsByRisk, inferPermissionMode, ruleForActions } from "./connector-token-permission-model";

const target = { connector_kind: "postgres", target_id: 7 };
const actions = [
  { name: "get_tables", risk: "read", category: "schema" },
  { name: "query_readonly", risk: "read", category: "schema" },
  { name: "create_user", risk: "write" },
];
const permissions = (rules) =>
  actions.map((action, index) => ({
    target_id: 7,
    profile_id: 11,
    action_name: action.name,
    execution_rule: rules[index],
    expires_at: "",
  }));

describe("connector permission mode classification", () => {
  it("keeps absent targets, empty catalogs, and uniform grants in Basic", () => {
    const uniform = permissions(["always_run", "always_run", "always_run"]);
    expect(inferPermissionMode(uniform, null, 11, actions)).toBe("basic");
    expect(inferPermissionMode(uniform, target, 11, [])).toBe("basic");
    expect(inferPermissionMode(uniform, target, 11, actions)).toBe("basic");
    expect(ruleForActions(uniform, null, 11, actions)).toBe("");
    expect(ruleForActions(uniform, target, 11, [])).toBe("");
  });

  it("distinguishes uniform risk groups from mixed grants inside one risk group", () => {
    expect(inferPermissionMode(permissions(["always_run", "always_run", "blocked"]), target, 11, actions)).toBe("grouped");
    expect(inferPermissionMode(permissions(["always_run", "approval_required", "blocked"]), target, 11, actions)).toBe("advanced");
    expect(ruleForActions(permissions(["always_run", "approval_required", "blocked"]), target, 12, actions)).toBe("");
  });

  it("preserves category insertion order and appends repeated categories", () => {
    expect(groupActions(actions)).toEqual([
      { name: "schema", actions: actions.slice(0, 2) },
      { name: "actions", actions: actions.slice(2) },
    ]);
    expect(groupActions([])).toEqual([]);
  });

  it("places every action into a canonical risk group, including missing and unknown risk", () => {
    const catalog = [...actions, { name: "unknown", risk: "unrecognized" }, { name: "missing" }];
    const groups = groupActionsByRisk(catalog);
    expect(groups.flatMap((group) => group.actions)).toHaveLength(catalog.length);
    expect(groups.find((group) => group.key === "write").actions).toEqual(catalog.slice(2, 3));
    expect(groups.find((group) => group.key === "other").actions).toEqual(catalog.slice(3));
    expect(groups.find((group) => group.key === "read").actions).toEqual(catalog.slice(0, 2));
  });
});
