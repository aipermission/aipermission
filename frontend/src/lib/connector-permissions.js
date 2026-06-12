import { effectiveRule } from "./permissions";

const profileStoragePrefix = "aipermission.console.profile";

export function profilesForConnectorTarget(targets, selectedTarget) {
  if (!selectedTarget) return [];
  const profiles = targets.filter(
    (target) => target.connector_kind === selectedTarget.connector_kind && Number(target.target_id) === Number(selectedTarget.target_id)
  );
  return profiles.length > 0 ? profiles : [selectedTarget];
}

export function selectedConnectorProfile(tokenID, selectedTarget, profiles, profileByToken = {}) {
  const id = selectedConnectorProfileID(tokenID, selectedTarget, profiles, profileByToken);
  return profiles.find((profile) => Number(profile.profile_id) === Number(id)) || profiles[0] || null;
}

export function selectedConnectorProfileID(tokenID, selectedTarget, profiles, profileByToken = {}) {
  if (!selectedTarget) return "";
  const stored = profileByToken[tokenID] || readStoredConnectorProfileID(selectedTarget, tokenID);
  const fallback = selectedTarget.profile_id || profiles[0]?.profile_id || "";
  const candidate = stored || fallback;
  return profiles.some((profile) => Number(profile.profile_id) === Number(candidate)) ? Number(candidate) : Number(fallback);
}

export function readStoredConnectorProfileID(target, tokenID) {
  if (!target || typeof window === "undefined") return "";
  const value = window.localStorage.getItem(connectorProfileStorageKey(target, tokenID));
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : "";
}

export function writeStoredConnectorProfileID(target, tokenID, profileID) {
  if (!target || typeof window === "undefined") return;
  window.localStorage.setItem(connectorProfileStorageKey(target, tokenID), String(profileID));
}

export function currentConnectorTargetProfilePermissions(permissions, target, profileID) {
  if (!target) return [];
  return permissions.filter((permission) => matchesConnectorTargetProfile(permission, target, profileID));
}

export function effectiveConnectorTargetProfilePermissions(permissions, target, profileID, now = Date.now()) {
  return currentConnectorTargetProfilePermissions(permissions, target, profileID).filter((permission) => effectiveRule(permission, now));
}

export function matchesConnectorTargetProfile(permission, target, profileID) {
  return Number(permission.target_id) === Number(target.target_id) && Number(permission.profile_id) === Number(profileID);
}

export function matchesConnectorTargetProfileAction(permission, target, profileID, actionName) {
  return matchesConnectorTargetProfile(permission, target, profileID) && permission.action_name === actionName;
}

export function connectorTargetProfileLifetime(permissions, target, profileID) {
  const active = currentConnectorTargetProfilePermissions(permissions, target, profileID).filter((permission) => effectiveRule(permission));
  if (active.length === 0) return null;
  const expiring = active.find((permission) => permission.expires_at);
  return expiring || active[0];
}

function connectorProfileStorageKey(target, tokenID) {
  return `${profileStoragePrefix}:${target.connector_kind}:${target.target_id}:${tokenID}`;
}
