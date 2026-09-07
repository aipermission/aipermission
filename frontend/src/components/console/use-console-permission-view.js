import { useMemo } from "react";
import {
  currentConnectorTargetProfilePermissions,
  effectiveConnectorTargetProfilePermissions,
  selectedConnectorProfileID,
} from "../../lib/connector-permissions";
import { effectiveRule, permissionLifetimeLabel } from "../../lib/permissions";

export function useConsolePermissionView({ connectorPermissions, mcpEnabled, now, profiles, target, tokens }) {
  return useMemo(
    () => deriveConsolePermissionView({ connectorPermissions, mcpEnabled, now, profiles, target, tokens }),
    [connectorPermissions, mcpEnabled, now, profiles, target, tokens],
  );
}

export function deriveConsolePermissionView({ connectorPermissions, mcpEnabled, now, profiles, target, tokens }) {
  if (!target) return emptyPermissionView;

  const selectedTokenOptions = tokens.filter((token) => {
    if (token.revoked_at) return false;
    const profileID = selectedConnectorProfileID(token.id, target, profiles);
    return effectiveConnectorTargetProfilePermissions(connectorPermissions[token.id] || [], target, profileID, now).some(
      (permission) => permission.project_enabled !== false,
    );
  });
  const alwaysRunTokenPermissions = selectedTokenOptions
    .map((token) => {
      const profileID = selectedConnectorProfileID(token.id, target, profiles);
      const permission = currentConnectorTargetProfilePermissions(connectorPermissions[token.id] || [], target, profileID).find(
        (item) => effectiveRule(item, now) === "always_run",
      );
      return permission ? { token, permission } : null;
    })
    .filter(Boolean);
  const temporaryAlwaysRunLabels = alwaysRunTokenPermissions
    .map(({ permission }) => permission)
    .filter((permission) => permission.expires_at)
    .map((permission) => permissionLifetimeLabel(permission, now));

  return {
    alwaysRunTokenPermissions,
    selectedTokenOptions,
    showAlwaysRunWarning: Boolean(mcpEnabled && alwaysRunTokenPermissions.length > 0),
    temporaryAlwaysRunLabels,
  };
}

const emptyPermissionView = Object.freeze({
  alwaysRunTokenPermissions: [],
  selectedTokenOptions: [],
  showAlwaysRunWarning: false,
  temporaryAlwaysRunLabels: [],
});
