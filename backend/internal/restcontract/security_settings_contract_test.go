package restcontract

import "testing"

func TestSecuritySettingsUpdateAlternativesRemainTypedObjects(t *testing.T) {
	schema := securitySettingsUpdateSchema()
	alternatives := schema["anyOf"].([]any)
	if len(alternatives) != 2 {
		t.Fatalf("revision alternatives=%#v", alternatives)
	}
	for _, alternative := range alternatives {
		branch := alternative.(map[string]any)
		if branch["type"] != "object" || branch["additionalProperties"] != false {
			t.Fatalf("revision alternative lost its typed settings object: %#v", branch)
		}
		properties := branch["properties"].(map[string]any)
		for _, field := range []string{"reusable_tokens", "expose_mcp_server_metadata", "mcp_start_enabled", "redaction_mode", "expected_revision", "revision"} {
			if properties[field] == nil {
				t.Fatalf("typed revision alternative lost field %s", field)
			}
		}
	}
}

func TestSecuritySettingsUpdatePreservesRevisionAndFieldValidation(t *testing.T) {
	schema := securitySettingsUpdateSchema()
	common := map[string]any{
		"reusable_tokens": true, "expose_mcp_server_metadata": false, "mcp_start_enabled": true, "redaction_mode": "basic",
	}
	for _, revisions := range []map[string]any{
		{"expected_revision": "r1"}, {"revision": "r1"}, {"expected_revision": "r1", "revision": "r1"},
	} {
		request := settingsTestRequest(common, revisions)
		if err := validateSchemaValue("$", request, schema, nil); err != nil {
			t.Fatalf("valid settings update rejected: %#v: %v", request, err)
		}
	}
	for _, request := range []any{
		common, map[string]any{"expected_revision": "r1"}, "r1", nil,
		settingsTestRequest(common, map[string]any{"expected_revision": " "}),
		settingsTestRequest(common, map[string]any{"revision": 1}),
		settingsTestRequest(common, map[string]any{"expected_revision": "r1", "reusable_tokens": "true"}),
		settingsTestRequest(common, map[string]any{"revision": "r1", "redaction_mode": "unknown"}),
		settingsTestRequest(common, map[string]any{"revision": "r1", "unexpected": true}),
	} {
		if err := validateSchemaValue("$", request, schema, nil); err == nil {
			t.Fatalf("invalid settings update accepted: %#v", request)
		}
	}
}

func settingsTestRequest(common, overrides map[string]any) map[string]any {
	request := make(map[string]any, len(common)+len(overrides))
	for key, value := range common {
		request[key] = value
	}
	for key, value := range overrides {
		request[key] = value
	}
	return request
}
