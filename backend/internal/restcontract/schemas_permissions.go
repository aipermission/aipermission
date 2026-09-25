package restcontract

func permissionSchemas() map[string]any {
	return map[string]any{
		"ConnectorPermissionInput": objectSchema(map[string]any{
			"target_id": integerSchema(), "profile_id": integerSchema(), "action_name": nonBlankStringSchema(),
			"execution_rule": enumSchema(ConnectorExecutionRules()...), "expires_at": stringSchema(),
		}, []string{"target_id", "profile_id", "action_name", "execution_rule"}),
		"ConnectorPermission": objectSchema(map[string]any{
			"project_id": integerSchema(), "project_name": stringSchema(), "project_slug": stringSchema(), "project_enabled": boolSchema(),
			"target_id": integerSchema(), "target_name": stringSchema(), "profile_id": integerSchema(), "profile_label": stringSchema(),
			"target_ref": stringSchema(), "connector_kind": stringSchema(), "profile_kind": stringSchema(),
			"action_name": nonBlankStringSchema(), "execution_rule": enumSchema(ConnectorExecutionRules()...), "expires_at": stringSchema(),
			"created_at": dateTimeSchema(), "updated_at": dateTimeSchema(),
		}, []string{
			"project_id", "project_name", "project_slug", "project_enabled", "target_id", "target_name", "profile_id", "profile_label",
			"target_ref", "connector_kind", "profile_kind", "action_name", "execution_rule", "created_at", "updated_at",
		}),
		"ConnectorPermissionsResponse": objectSchema(map[string]any{
			"items": arraySchema(refSchema("ConnectorPermission")), "revision": stringSchema(), "changed": boolSchema(),
		}, []string{"items", "revision"}),
		"UpdateConnectorPermissionsRequest": objectSchema(map[string]any{
			"permissions": arraySchema(refSchema("ConnectorPermissionInput")), "expected_revision": stringSchema(),
		}, []string{"permissions", "expected_revision"}),
		"TokenProjectScope": objectSchema(map[string]any{
			"project_id": integerSchema(), "project_name": stringSchema(), "project_slug": stringSchema(), "enabled": boolSchema(),
		}, []string{"project_id", "project_name", "project_slug", "enabled"}),
		"TokenProjectScopesResponse": objectSchema(map[string]any{
			"items": arraySchema(refSchema("TokenProjectScope")), "revision": stringSchema(), "changed": boolSchema(),
		}, []string{"items", "revision"}),
		"UpdateTokenProjectScopesRequest": objectSchema(map[string]any{
			"enabled_project_ids": arraySchema(integerSchema()), "expected_revision": stringSchema(),
		}, []string{"enabled_project_ids", "expected_revision"}),
	}
}
