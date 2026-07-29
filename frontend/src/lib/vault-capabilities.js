export function vaultCapabilityDraftFromItems(items, definitions) {
  return Object.fromEntries(
    (items || []).flatMap((capability) => {
      const definition = (definitions || []).find((item) => item.name === capability.capability_name);
      if (!definition?.allowed_rules?.includes(capability.execution_rule)) return [];
      return [[vaultCapabilityKey(capability.project_id, capability.capability_name), {
        execution_rule: capability.execution_rule,
        expires_at: capability.expires_at || "",
      }]];
    })
  );
}

export function vaultCapabilitiesFromDraft(projects, definitions, draft) {
  return (projects || []).flatMap((project) =>
    (definitions || []).flatMap((definition) => {
      const permission = draft[vaultCapabilityKey(project.project_id, definition.name)];
      if (!permission?.execution_rule) return [];
      return [{
        project_id: project.project_id,
        capability_name: definition.name,
        execution_rule: permission.execution_rule,
        expires_at: permission.expires_at || undefined,
      }];
    })
  );
}

export function vaultCapabilityKey(projectID, capabilityName) {
  return `${projectID}:${capabilityName}`;
}
