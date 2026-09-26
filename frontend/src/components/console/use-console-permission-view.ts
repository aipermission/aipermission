import { useMemo } from "react";
import {
  currentConnectorTargetProfilePermissions,
  effectiveConnectorTargetProfilePermissions,
  selectedConnectorProfileID,
} from "../../lib/connector-permissions";
import { effectiveRule, permissionLifetimeLabel } from "../../lib/permissions";
import type { TokenActionPermission } from "../../lib/gateway-contracts/security-contracts.ts";

type Token = { id: number; name: string; revoked_at?: string };
type Permission = Pick<TokenActionPermission, "target_id" | "profile_id" | "action_name" | "execution_rule" | "expires_at" | "project_enabled">;
type Props<Item extends Token> = {
  connectorPermissions: Record<string, Permission[]>;
  mcpEnabled: boolean;
  now: number;
  profiles: Parameters<typeof selectedConnectorProfileID>[2];
  target: Parameters<typeof selectedConnectorProfileID>[1];
  tokens: Item[];
};

export function useConsolePermissionView<Item extends Token>({ connectorPermissions, mcpEnabled, now, profiles, target, tokens }: Props<Item>) {
  return useMemo(
    () => deriveConsolePermissionView({ connectorPermissions, mcpEnabled, now, profiles, target, tokens }),
    [connectorPermissions, mcpEnabled, now, profiles, target, tokens],
  );
}

export function deriveConsolePermissionView<Item extends Token>({ connectorPermissions, mcpEnabled, now, profiles, target, tokens }: Props<Item>) {
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
        (item) => item.project_enabled !== false && effectiveRule(item, now) === "always_run",
      );
      return permission ? { token, permission } : null;
    })
    .filter((entry) => entry !== null);
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
