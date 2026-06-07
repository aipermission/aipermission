export function permissionsToMap(permissions) {
  return Object.fromEntries(
    permissions.map((permission) => [
      permission.server_id,
      {
        execution_rule: permission.execution_rule,
        expires_at: permission.expires_at || "",
      },
    ])
  );
}

export const permissionLifetimeOptions = [
  { value: "permanent", label: "Permanent", ms: 0 },
  { value: "1h", label: "1 hour", ms: 60 * 60 * 1000 },
  { value: "4h", label: "4 hours", ms: 4 * 60 * 60 * 1000 },
  { value: "1d", label: "1 day", ms: 24 * 60 * 60 * 1000 },
];

export function normalizePermission(value) {
  if (!value) return null;
  if (typeof value === "string") {
    return { execution_rule: value, expires_at: "" };
  }
  return {
    execution_rule: value.execution_rule || "",
    expires_at: value.expires_at || "",
  };
}

export function permissionExpired(value, now = Date.now()) {
  const permission = normalizePermission(value);
  if (!permission?.expires_at) return false;
  const expiresAt = new Date(permission.expires_at).getTime();
  return Number.isFinite(expiresAt) && expiresAt <= now;
}

export function effectiveRule(value, now = Date.now()) {
  const permission = normalizePermission(value);
  if (!permission || permissionExpired(permission, now)) return "";
  return permission.execution_rule;
}

export function permissionLifetimeLabel(value, now = Date.now()) {
  const permission = normalizePermission(value);
  if (!permission?.expires_at) return "Permanent";
  const expiresAt = new Date(permission.expires_at).getTime();
  if (!Number.isFinite(expiresAt)) return "Permanent";
  const diff = expiresAt - now;
  if (diff <= 0) return "Expired";
  const minutes = Math.max(1, Math.round(diff / 60000));
  if (minutes < 60) return `${minutes}m left`;
  const hours = Math.max(1, Math.round(minutes / 60));
  if (hours < 24) return `${hours}h left`;
  const days = Math.max(1, Math.round(hours / 24));
  return `${days}d left`;
}

export function expiresAtFromLifetime(value, now = Date.now()) {
  const option = permissionLifetimeOptions.find((item) => item.value === value);
  if (!option?.ms) return "";
  return new Date(now + option.ms).toISOString();
}

export function ruleLabel(rule) {
  if (rule === "always_run") return "always run";
  if (rule === "approval_required") return "prompt";
  return "disabled";
}

export function ruleDotClass(rule) {
  if (rule === "always_run") return "bg-emerald-500";
  if (rule === "approval_required") return "bg-amber-400";
  return "bg-red-500";
}

export function permissionCardClass(rule) {
  if (rule === "always_run") return "border-emerald-100 bg-emerald-50/45 permission-card-good";
  if (rule === "approval_required") return "border-amber-100 bg-amber-50/45 permission-card-warn";
  return "border-red-100 bg-red-50/35 permission-card-bad";
}

export function maskedToken(value) {
  if (!value) return "legacy token unavailable";
  if (value.length <= 14) return value;
  return `${value.slice(0, 8)}...${value.slice(-6)}`;
}
