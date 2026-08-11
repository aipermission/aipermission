package restcontract

import "fmt"

type operationContract struct {
	StatusCode     string
	ResponseSchema map[string]any
}

func sharedSchemas() map[string]any {
	stringMap := objectSchema(map[string]any{}, nil)
	stringMap["additionalProperties"] = true
	return map[string]any{
		"Error": objectSchema(map[string]any{
			"error": stringSchema(),
			"code":  stringSchema(),
		}, []string{"error"}),
		"ConnectorActionDefinition": objectSchema(map[string]any{
			"name":                   stringSchema(),
			"label":                  stringSchema(),
			"description":            stringSchema(),
			"category":               stringSchema(),
			"risk":                   enumSchema("read", "write", "destructive", "credential_sensitive"),
			"input_schema":           stringMap,
			"sensitive_input_fields": arraySchema(stringSchema()),
			"output_hint":            stringMap,
		}, []string{"name", "label", "description", "risk", "input_schema"}),
		"ConnectorCredentialProfile": objectSchema(map[string]any{
			"id":                      integerSchema(),
			"target_id":               integerSchema(),
			"ref":                     stringSchema(),
			"connector_kind":          stringSchema(),
			"kind":                    stringSchema(),
			"label":                   stringSchema(),
			"public":                  stringMap,
			"risk_label":              stringSchema(),
			"actions":                 arraySchema(refSchema("ConnectorActionDefinition")),
			"runtime_id":              integerSchema(),
			"transfer_runtime_id":     integerSchema(),
			"vault_session_supported": boolSchema(),
			"created_at":              dateTimeSchema(),
			"updated_at":              dateTimeSchema(),
		}, []string{"id", "target_id", "connector_kind", "kind", "label", "vault_session_supported", "created_at", "updated_at"}),
		"ConnectorTarget": objectSchema(map[string]any{
			"id":             integerSchema(),
			"project_id":     integerSchema(),
			"project_name":   stringSchema(),
			"project_slug":   stringSchema(),
			"ref":            stringSchema(),
			"connector_kind": stringSchema(),
			"name":           stringSchema(),
			"config":         stringMap,
			"status":         stringSchema(),
			"created_at":     dateTimeSchema(),
			"updated_at":     dateTimeSchema(),
			"profiles":       arraySchema(refSchema("ConnectorCredentialProfile")),
		}, []string{"id", "project_id", "project_name", "project_slug", "connector_kind", "name", "status", "created_at", "updated_at"}),
		"TargetProfile": objectSchema(map[string]any{
			"ref":                 stringSchema(),
			"project_id":          integerSchema(),
			"project_name":        stringSchema(),
			"project_slug":        stringSchema(),
			"connector_kind":      stringSchema(),
			"target_id":           integerSchema(),
			"target_name":         stringSchema(),
			"profile_id":          integerSchema(),
			"profile_kind":        stringSchema(),
			"profile_label":       stringSchema(),
			"runtime_id":          integerSchema(),
			"transfer_runtime_id": integerSchema(),
			"config":              stringMap,
			"public":              stringMap,
			"status":              stringSchema(),
			"created_at":          dateTimeSchema(),
			"updated_at":          dateTimeSchema(),
		}, []string{"ref", "project_id", "project_name", "project_slug", "connector_kind", "target_id", "target_name", "profile_id", "profile_kind", "profile_label", "status", "created_at", "updated_at"}),
		"ConnectorActionApproval": objectSchema(map[string]any{
			"id":                    integerSchema(),
			"token_id":              nullableIntegerSchema(),
			"token_name":            stringSchema(),
			"target_id":             integerSchema(),
			"target_name":           stringSchema(),
			"target_ref":            stringSchema(),
			"profile_id":            integerSchema(),
			"profile_label":         stringSchema(),
			"connector_kind":        stringSchema(),
			"action_name":           stringSchema(),
			"title":                 stringSchema(),
			"summary":               stringSchema(),
			"preview":               stringMap,
			"input":                 stringMap,
			"reason":                stringSchema(),
			"status":                actionStatusSchema(),
			"output":                map[string]any{},
			"display_text":          stringSchema(),
			"error":                 stringSchema(),
			"approval_context_hash": stringSchema(),
			"created_at":            dateTimeSchema(),
			"completed_at":          nullableDateTimeSchema(),
			"retry_after_seconds":   integerSchema(),
			"assistant_hint":        stringSchema(),
		}, []string{"id", "target_id", "target_name", "target_ref", "profile_id", "profile_label", "connector_kind", "action_name", "status", "created_at"}),
		"HistoryEntry": historyEntrySchema(stringMap),
		"AuditEntry":   auditEntrySchema(),
		"HistoryPage":  pageSchema(refSchema("HistoryEntry")),
		"AuditPage":    pageSchema(refSchema("AuditEntry")),
	}
}

func typedOperationContracts() map[Route]operationContract {
	return map[Route]operationContract{
		{Method: "GET", Path: "/api/targets"}:                                              okContract(arraySchema(refSchema("TargetProfile"))),
		{Method: "GET", Path: "/api/connector-targets"}:                                    okContract(itemsSchema(refSchema("ConnectorTarget"))),
		{Method: "GET", Path: "/api/connector-targets/{id}"}:                               okContract(refSchema("ConnectorTarget")),
		{Method: "GET", Path: "/api/connector-targets/{id}/profiles"}:                      okContract(itemsSchema(refSchema("ConnectorCredentialProfile"))),
		{Method: "GET", Path: "/api/connector-targets/{id}/profiles/{profile_id}/actions"}: okContract(itemsSchema(refSchema("ConnectorActionDefinition"))),
		{Method: "GET", Path: "/api/connector-action-approvals"}:                           okContract(arraySchema(refSchema("ConnectorActionApproval"))),
		{Method: "GET", Path: "/api/connector-action-approvals/{id}"}:                      okContract(refSchema("ConnectorActionApproval")),
		{Method: "POST", Path: "/api/connector-action-approvals/{id}/run"}:                 okContract(refSchema("ConnectorActionApproval")),
		{Method: "POST", Path: "/api/connector-action-approvals/{id}/decline"}:             okContract(refSchema("ConnectorActionApproval")),
		{Method: "GET", Path: "/api/history"}:                                              okContract(refSchema("HistoryPage")),
		{Method: "GET", Path: "/api/history/{id}"}:                                         okContract(refSchema("HistoryEntry")),
		{Method: "GET", Path: "/api/audit-logs"}:                                           okContract(refSchema("AuditPage")),
		{Method: "GET", Path: "/api/audit-logs/{id}"}:                                      okContract(refSchema("AuditEntry")),
	}
}

// ValidateTypedRoutes ensures the hand-reviewed typed subset cannot silently
// drift away from the registered route inventory.
func ValidateTypedRoutes(routes []Route) error {
	registered := make(map[Route]struct{}, len(routes))
	for _, route := range routes {
		registered[route] = struct{}{}
	}
	for route := range typedOperationContracts() {
		if _, ok := registered[route]; !ok {
			return fmt.Errorf("typed contract references unregistered route %s %s", route.Method, route.Path)
		}
	}
	return nil
}

func okContract(schema map[string]any) operationContract {
	return operationContract{StatusCode: "200", ResponseSchema: schema}
}

func historyEntrySchema(freeObject map[string]any) map[string]any {
	properties := map[string]any{
		"id": integerSchema(), "source_ref_type": stringSchema(), "source_ref_id": integerSchema(),
		"connector_kind": stringSchema(), "activity_type": stringSchema(), "token_id": nullableIntegerSchema(),
		"token_name": stringSchema(), "project_id": nullableIntegerSchema(), "project_name": stringSchema(),
		"runtime_id": nullableIntegerSchema(), "target_id": nullableIntegerSchema(), "profile_id": nullableIntegerSchema(),
		"target_name": stringSchema(), "profile_label": stringSchema(), "source": stringSchema(),
		"status": actionStatusSchema(), "action_name": stringSchema(), "title": stringSchema(), "summary": stringSchema(),
		"preview_json": stringSchema(), "input_text": stringSchema(), "input_json": stringSchema(),
		"output_text": stringSchema(), "output_json": stringSchema(), "error": stringSchema(),
		"exit_code": nullableIntegerSchema(), "progress_current": integerSchema(), "progress_total": integerSchema(),
		"bytes_done": integerSchema(), "bytes_total": integerSchema(), "approval_required": boolSchema(),
		"user_note": stringSchema(), "created_at": dateTimeSchema(), "started_at": nullableDateTimeSchema(),
		"completed_at": nullableDateTimeSchema(), "updated_at": dateTimeSchema(), "labels": arraySchema(freeObject),
	}
	return objectSchema(properties, []string{"id", "source_ref_type", "source_ref_id", "connector_kind", "activity_type", "target_name", "source", "status", "action_name", "title", "summary", "progress_current", "progress_total", "bytes_done", "bytes_total", "approval_required", "created_at", "updated_at", "labels"})
}

func auditEntrySchema() map[string]any {
	return objectSchema(map[string]any{
		"id": integerSchema(), "actor_type": stringSchema(), "token_id": nullableIntegerSchema(), "token_name": stringSchema(),
		"project_id": nullableIntegerSchema(), "project_name": stringSchema(), "runtime_id": nullableIntegerSchema(),
		"connector_kind": stringSchema(), "target_id": nullableIntegerSchema(), "target_name": stringSchema(),
		"profile_id": nullableIntegerSchema(), "action_request_id": nullableIntegerSchema(), "action": stringSchema(),
		"payload_json": stringSchema(), "created_at": dateTimeSchema(),
	}, []string{"id", "actor_type", "action", "payload_json", "created_at"})
}

func pageSchema(item map[string]any) map[string]any {
	return objectSchema(map[string]any{
		"items": arraySchema(item), "total": integerSchema(), "limit": integerSchema(), "offset": integerSchema(),
		"next_offset": nullableIntegerSchema(),
	}, []string{"items", "total", "limit", "offset"})
}

func itemsSchema(item map[string]any) map[string]any {
	return objectSchema(map[string]any{"items": arraySchema(item)}, []string{"items"})
}

func objectSchema(properties map[string]any, required []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func arraySchema(items map[string]any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}
func stringSchema() map[string]any   { return map[string]any{"type": "string"} }
func integerSchema() map[string]any  { return map[string]any{"type": "integer", "format": "int64"} }
func boolSchema() map[string]any     { return map[string]any{"type": "boolean"} }
func dateTimeSchema() map[string]any { return map[string]any{"type": "string", "format": "date-time"} }
func nullableIntegerSchema() map[string]any {
	return map[string]any{"type": []string{"integer", "null"}, "format": "int64"}
}
func nullableDateTimeSchema() map[string]any {
	return map[string]any{"type": []string{"string", "null"}, "format": "date-time"}
}
func refSchema(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}
func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func actionStatusSchema() map[string]any {
	return enumSchema("completed", "failed", "canceled", "running", "approval_pending", "blocked", "stale", "declined", "error", "outcome_unknown", "pending", "paused", "untracked")
}
