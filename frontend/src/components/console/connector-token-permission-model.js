import {
  connectorActionRiskDescription,
  connectorActionRiskGroupLabel,
  connectorActionRiskOrder,
  normalizeConnectorActionRisk,
} from "../../lib/connector-action-risks";
import { matchesConnectorTargetProfileAction } from "../../lib/connector-permissions";
import { effectiveRule } from "../../lib/permissions";
import { getConnectorModel } from "../../connectors/templates/registry";

export function matchesPermissionMutationError(value, tokenID, profileID, targetKey) {
  return value?.targetKey === targetKey && Number(value?.tokenID) === Number(tokenID) && Number(value?.profileID) === Number(profileID);
}

export function targetSupportsMessages(target) {
  if (!target?.runtime_id) return false;
  return Boolean(getConnectorModel(target.connector_kind)?.usesLiveConsole?.({ target }));
}

export function ruleForActions(permissions, target, profileID, actions) {
  if (!target || actions.length === 0) return "";
  const rules = actions.map((action) => {
    const permission = permissions.find((item) => matchesConnectorTargetProfileAction(item, target, profileID, action.name));
    return effectiveRule(permission) || "";
  });
  const unique = new Set(rules);
  return unique.size <= 1 ? rules[0] || "" : "mixed";
}

export function inferPermissionMode(permissions, target, profileID, actions) {
  if (!target || actions.length === 0) return "basic";
  if (ruleForActions(permissions, target, profileID, actions) !== "mixed") return "basic";
  const riskGroups = groupActionsByRisk(actions).filter((group) => group.actions.length > 0);
  if (riskGroups.length === 0) return "basic";
  return riskGroups.every((group) => ruleForActions(permissions, target, profileID, group.actions) !== "mixed") ? "grouped" : "advanced";
}

export function tokenProfileModeKey(tokenID, target, profileID) {
  return `${tokenID}:${target?.connector_kind || ""}:${target?.target_id || ""}:${profileID || ""}`;
}

export function groupActions(actions) {
  const order = [];
  const groups = new Map();
  for (const action of actions) {
    const name = action.category || "actions";
    if (!groups.has(name)) {
      groups.set(name, []);
      order.push(name);
    }
    groups.get(name).push(action);
  }
  return order.map((name) => ({ name, actions: groups.get(name) || [] }));
}

export function groupActionsByRisk(actions) {
  const grouped = new Map(connectorActionRiskOrder.map((risk) => [risk, []]));
  for (const action of actions) {
    grouped.get(normalizeConnectorActionRisk(action.risk)).push(action);
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
