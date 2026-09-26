export type SecuritySettings = {
  reusable_tokens: boolean;
  expose_mcp_server_metadata: boolean;
  mcp_start_enabled: boolean;
  redaction_mode: "basic" | "off";
  revision: string;
};
export type RedactionForm = { name: string; pattern: string; enabled: boolean };
export type RedactionRule = RedactionForm & { id: number };

export function securitySettingsResponse(value: unknown): SecuritySettings {
  if (!value || typeof value !== "object" || Array.isArray(value)) throw new Error("Security settings response is invalid.");
  const data = value as Record<string, unknown>;
  const validMode = data.redaction_mode === "basic" || data.redaction_mode === "off";
  const validBooleans = ["reusable_tokens", "expose_mcp_server_metadata", "mcp_start_enabled"].every(
    (field) => typeof data[field] === "boolean",
  );
  if (!validMode || !validBooleans || typeof data.revision !== "string" || data.revision.trim() === "")
    throw new Error("Security settings response is invalid.");
  return data as SecuritySettings;
}

export function redactionRulesResponse(value: unknown): RedactionRule[] {
  if (!Array.isArray(value)) throw new Error("Redaction rules response is invalid.");
  return value.map((entry: unknown) => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw new Error("Redaction rule response is invalid.");
    const rule = entry as Record<string, unknown>;
    if (
      typeof rule.id !== "number" ||
      !Number.isSafeInteger(rule.id) ||
      rule.id <= 0 ||
      typeof rule.name !== "string" ||
      typeof rule.pattern !== "string" ||
      typeof rule.enabled !== "boolean"
    )
      throw new Error("Redaction rule response is invalid.");
    return rule as RedactionRule;
  });
}
