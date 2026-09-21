export const connectorActionResponse = {
  status: "completed",
  request_id: 7,
  target_ref: "redis:1:1",
  connector_kind: "redis",
  action_name: "get_string",
  retry_policy: { class: "read_only", guidance: "Read again if needed." },
};

export const connectorTargetResponse = {
  target_ref: "ssh:1:1",
  project_id: 1,
  project_name: "My Project",
  project_slug: "my-project",
  target_id: 1,
  target_name: "server",
  connector_kind: "ssh",
  profile_id: 1,
  profile_label: "admin",
  profile_kind: "private_key",
  actions: [],
};

export function vaultActionResponse(overrides = {}) {
  return {
    status: "completed",
    request_id: 9,
    project_ref: "my-project",
    action_name: "generate_item",
    secret_values_returned: false,
    ...overrides,
  };
}
