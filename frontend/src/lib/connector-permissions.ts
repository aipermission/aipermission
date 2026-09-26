import { effectiveRule } from "./permissions";
import { readLocalPreference, writeLocalPreference } from "./browser-storage.ts";
import type { TokenActionPermission } from "./gateway-contracts/security-contracts";

const profileStoragePrefix = "aipermission.console.profile";

type Target = { connector_kind?: string; target_id?: number; id?: number; profile_id?: number };
type Profile = Target & { profile_id: number };
type Permission = Pick<TokenActionPermission, "target_id" | "profile_id" | "action_name" | "execution_rule" | "expires_at">;
type TokenID = number | string;

export function connectorTargetKey(target: Target | null | undefined): string {
  if (!target) return "";
  return `${target.connector_kind || "connector"}:${target.target_id || target.id || ""}`;
}

export function profilesForConnectorTarget<T extends Target>(targets: T[], selectedTarget: T | null): T[] {
  if (!selectedTarget) return [];
  const selectedKey = connectorTargetKey(selectedTarget);
  const profiles = targets.filter((target) => connectorTargetKey(target) === selectedKey);
  return profiles.length > 0 ? profiles : [selectedTarget];
}

export function selectedConnectorProfile<T extends Profile>(
  tokenID: TokenID,
  selectedTarget: Target | null,
  profiles: T[],
  profileByToken: Record<string, number | string> = {},
): T | null {
  const id = selectedConnectorProfileID(tokenID, selectedTarget, profiles, profileByToken);
  return profiles.find((profile) => Number(profile.profile_id) === Number(id)) || null;
}

export function selectedConnectorProfileID(
  tokenID: TokenID,
  selectedTarget: Target | null,
  profiles: Profile[],
  profileByToken: Record<string, number | string> = {},
): number | "" {
  if (!selectedTarget) return "";
  const stored = profileByToken[tokenID] || readStoredConnectorProfileID(selectedTarget, tokenID);
  const fallback = selectedTarget.profile_id || (profiles.length === 1 ? profiles[0]?.profile_id : "") || "";
  const candidate = stored || fallback;
  if (profiles.some((profile) => Number(profile.profile_id) === Number(candidate))) return Number(candidate);
  return fallback ? Number(fallback) : "";
}

export function readStoredConnectorProfileID(target: Target | null, tokenID: TokenID): number | "" {
  if (!target || typeof window === "undefined") return "";
  const value = readLocalPreference(connectorProfileStorageKey(target, tokenID));
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : "";
}

export function writeStoredConnectorProfileID(target: Target | null, tokenID: TokenID, profileID: number | string): void {
  if (!target || typeof window === "undefined") return;
  writeLocalPreference(connectorProfileStorageKey(target, tokenID), String(profileID));
}

export function currentConnectorTargetProfilePermissions<Item extends Permission>(
  permissions: Item[],
  target: Target | null,
  profileID: number | string,
): Item[] {
  if (!target) return [];
  return permissions.filter((permission) => matchesConnectorTargetProfile(permission, target, profileID));
}

export function effectiveConnectorTargetProfilePermissions<Item extends Permission>(
  permissions: Item[],
  target: Target | null,
  profileID: number | string,
  now = Date.now(),
): Item[] {
  return currentConnectorTargetProfilePermissions(permissions, target, profileID).filter((permission) => effectiveRule(permission, now));
}

export function matchesConnectorTargetProfile(permission: Permission, target: Target, profileID: number | string): boolean {
  return Number(permission.target_id) === Number(target.target_id) && Number(permission.profile_id) === Number(profileID);
}

export function matchesConnectorTargetProfileAction(
  permission: Permission,
  target: Target,
  profileID: number | string,
  actionName: string,
): boolean {
  return matchesConnectorTargetProfile(permission, target, profileID) && permission.action_name === actionName;
}

export function connectorTargetProfileLifetime(
  permissions: Permission[],
  target: Target | null,
  profileID: number | string,
): Permission | null {
  const active = currentConnectorTargetProfilePermissions(permissions, target, profileID).filter((permission) => {
    const rule = effectiveRule(permission);
    return rule && rule !== "blocked";
  });
  if (active.length === 0) return null;
  const expiring = active.find((permission) => permission.expires_at);
  return expiring || active[0];
}

function connectorProfileStorageKey(target: Target, tokenID: TokenID): string {
  return `${profileStoragePrefix}:${target.connector_kind}:${target.target_id}:${tokenID}`;
}
