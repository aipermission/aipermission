export type VaultCapabilityDefinition = { name: string; allowed_rules?: readonly string[] };
export type VaultCapabilityGrant = { project_id: number; capability_name: string; execution_rule: string; expires_at?: string | null };
export type VaultCapabilityDraft = Record<string, { execution_rule: string; expires_at: string }>;

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
