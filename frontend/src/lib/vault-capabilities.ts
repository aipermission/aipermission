export type VaultCapabilityDefinition = { name: string; label: string; description: string; allowed_rules: readonly string[] };
export type VaultCapabilityGrant = { project_id: number; capability_name: string; execution_rule: string; expires_at?: string | null };
export type VaultCapabilityDraft = Record<string, { execution_rule: string; expires_at: string }>;
export type VaultCapabilitySnapshot = {
  definitions: VaultCapabilityDefinition[];
  items: VaultCapabilityGrant[];
  revision: string;
};

export function vaultCapabilitySnapshot(value: unknown): VaultCapabilitySnapshot {
  const invalid = () => new Error("Invalid Vault capability response from gateway.");
  if (!value || typeof value !== "object" || Array.isArray(value)) throw invalid();
  const response = value as Record<string, unknown>;
  if (
    typeof response.revision !== "string" ||
    !response.revision ||
    !Array.isArray(response.definitions) ||
    !Array.isArray(response.items)
  ) {
    throw invalid();
  }
  const definitions = response.definitions.map((entry: unknown): VaultCapabilityDefinition => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw invalid();
    const definition = entry as Record<string, unknown>;
    if (
      typeof definition.name !== "string" ||
      !definition.name ||
      typeof definition.label !== "string" ||
      typeof definition.description !== "string" ||
      !Array.isArray(definition.allowed_rules) ||
      !definition.allowed_rules.every((rule: unknown) => rule === "approval_required" || rule === "always_run")
    )
      throw invalid();
    return definition as VaultCapabilityDefinition;
  });
  const names = new Set(definitions.map((definition) => definition.name));
  const items = response.items.map((entry: unknown): VaultCapabilityGrant => {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) throw invalid();
    const item = entry as Record<string, unknown>;
    if (
      !Number.isSafeInteger(item.project_id) ||
      Number(item.project_id) <= 0 ||
      typeof item.capability_name !== "string" ||
      !names.has(item.capability_name) ||
      (item.execution_rule !== "approval_required" && item.execution_rule !== "always_run") ||
      (item.expires_at !== undefined && item.expires_at !== null && typeof item.expires_at !== "string")
    )
      throw invalid();
    return item as VaultCapabilityGrant;
  });
  return { definitions, items, revision: response.revision };
}

export function vaultCapabilityDraftFromItems(
  items: readonly VaultCapabilityGrant[] | null | undefined,
  definitions: readonly VaultCapabilityDefinition[] | null | undefined,
): VaultCapabilityDraft {
  return Object.fromEntries(
    (items || []).flatMap((capability) => {
      const definition = (definitions || []).find((item) => item.name === capability.capability_name);
      if (!definition?.allowed_rules?.includes(capability.execution_rule)) return [];
      return [
        [
          vaultCapabilityKey(capability.project_id, capability.capability_name),
          {
            execution_rule: capability.execution_rule,
            expires_at: capability.expires_at || "",
          },
        ],
      ];
    }),
  );
}

export function vaultCapabilitiesFromDraft(
  projects: readonly { project_id: number }[] | null | undefined,
  definitions: readonly VaultCapabilityDefinition[] | null | undefined,
  draft: VaultCapabilityDraft,
): VaultCapabilityGrant[] {
  return (projects || []).flatMap((project) =>
    (definitions || []).flatMap((definition) => {
      const permission = draft[vaultCapabilityKey(project.project_id, definition.name)];
      if (!permission?.execution_rule) return [];
      return [
        {
          project_id: project.project_id,
          capability_name: definition.name,
          execution_rule: permission.execution_rule,
          expires_at: permission.expires_at || undefined,
        },
      ];
    }),
  );
}

export function vaultCapabilityKey(projectID: number, capabilityName: string): string {
  return `${projectID}:${capabilityName}`;
}
