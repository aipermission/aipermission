import {
  connectorActionRiskDescription,
  connectorActionRiskGroupLabel,
  connectorActionRiskOrder,
  normalizeConnectorActionRisk,
} from "../../lib/connector-action-risks";
import { matchesConnectorTargetProfileAction } from "../../lib/connector-permissions";
import { effectiveRule } from "../../lib/permissions";
import { getConnectorModel } from "../../connectors/templates/registry";
import type { TokenActionPermission } from "../../lib/gateway-contracts/security-contracts";

type Target = { connector_kind?: string; target_id?: number; runtime_id?: number };
type Action = { name: string; category?: string; risk?: string };
type Permission = Pick<TokenActionPermission, "target_id" | "profile_id" | "action_name" | "execution_rule" | "expires_at">;
type MutationError = { targetKey?: string; tokenID?: number; profileID?: number } | null | undefined;

export type PermissionMode = "basic" | "grouped" | "advanced";

export function matchesPermissionMutationError(value: MutationError, tokenID: number, profileID: number, targetKey: string): boolean {
  return value?.targetKey === targetKey && Number(value?.tokenID) === Number(tokenID) && Number(value?.profileID) === Number(profileID);
}

export function targetSupportsMessages(target: Target | null | undefined): boolean {
  if (!target?.runtime_id) return false;
  return Boolean(getConnectorModel(target.connector_kind)?.usesLiveConsole?.({ target }));
}

export function ruleForActions(
  permissions: readonly Permission[],
  target: Target | null | undefined,
  profileID: number,
  actions: readonly Action[],
): string {
  if (!target || actions.length === 0) return "";
  const rules = actions.map((action) => {
    const permission = permissions.find((item) => matchesConnectorTargetProfileAction(item, target, profileID, action.name));
    return effectiveRule(permission) || "";
  });
  const unique = new Set(rules);
  return unique.size <= 1 ? rules[0] || "" : "mixed";
}

export function inferPermissionMode(
  permissions: readonly Permission[],
  target: Target | null | undefined,
  profileID: number,
  actions: readonly Action[],
): PermissionMode {
  if (!target || actions.length === 0) return "basic";
  if (ruleForActions(permissions, target, profileID, actions) !== "mixed") return "basic";
  const riskGroups = groupActionsByRisk(actions).filter((group) => group.actions.length > 0);
  return riskGroups.every((group) => ruleForActions(permissions, target, profileID, group.actions) !== "mixed") ? "grouped" : "advanced";
}

export function tokenProfileModeKey(tokenID: number, target: Target | null | undefined, profileID: number | string): string {
  return `${tokenID}:${target?.connector_kind || ""}:${target?.target_id || ""}:${profileID || ""}`;
}

export function groupActions<T extends Action>(actions: readonly T[]): { name: string; actions: T[] }[] {
  const order: string[] = [];
  const groups = new Map<string, T[]>();
  for (const action of actions) {
    const name = action.category || "actions";
    const group = groups.get(name);
    if (group) {
      group.push(action);
    } else {
      groups.set(name, [action]);
      order.push(name);
    }
  }
  return order.map((name) => ({ name, actions: groups.get(name) || [] }));
}

export function groupActionsByRisk<T extends Action>(actions: readonly T[]) {
  const grouped = new Map<string, T[]>();
  for (const action of actions) {
    const risk = normalizeConnectorActionRisk(action.risk);
    const group = grouped.get(risk) || [];
    group.push(action);
    grouped.set(risk, group);
  }
  return connectorActionRiskOrder.map((risk) => {
    const actionsForRisk = grouped.get(risk) || [];
    return {
      key: risk,
      name: connectorActionRiskGroupLabel(risk),
      description: connectorActionRiskDescription(risk, actionsForRisk.length),
      actions: actionsForRisk,
    };
  });
}
