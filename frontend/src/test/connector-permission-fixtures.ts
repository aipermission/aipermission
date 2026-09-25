export function connectorPermissionFixture(overrides: Record<string, unknown> = {}) {
  return {
    project_id: 3,
    project_name: "My Project",
    project_slug: "my-project",
    project_enabled: true,
    target_id: 7,
    target_name: "My Target",
    profile_id: 9,
    profile_label: "default",
    target_ref: "example:7:9",
    connector_kind: "example",
    profile_kind: "default",
    action_name: "read",
    execution_rule: "approval_required",
    created_at: "2026-09-25T00:00:00Z",
    updated_at: "2026-09-25T00:00:00Z",
    ...overrides,
  };
}

export function connectorPermissionSnapshot(items: unknown[], revision: string) {
  return { items, revision };
}
