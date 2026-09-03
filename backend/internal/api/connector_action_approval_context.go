package api

import (
	"encoding/json"
	"reflect"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func connectorApprovalContext(prepared actions.PreparedRequest, token tokens.Token, permission connectortargets.ActionPermission, capturedAt string) (string, string, error) {
	payloadHashMaterial, err := json.Marshal(map[string]any{
		"input":   prepared.Requested.Input,
		"payload": prepared.Action.Payload,
	})
	if err != nil {
		return "", "", err
	}
	actionDefinition := map[string]any{
		"name":                   prepared.ActionDefinition.Name,
		"label":                  prepared.ActionDefinition.Label,
		"description":            prepared.ActionDefinition.Description,
		"category":               prepared.ActionDefinition.Category,
		"risk":                   prepared.ActionDefinition.Risk,
		"retry_policy":           connectors.EffectiveRetryPolicy(prepared.ActionDefinition),
		"input_schema":           prepared.ActionDefinition.InputSchema,
		"sensitive_input_fields": prepared.ActionDefinition.SensitiveInputFields,
		"output_hint":            prepared.ActionDefinition.OutputHint,
	}
	actionDefinitionHashMaterial, err := json.Marshal(actionDefinition)
	if err != nil {
		return "", "", err
	}
	contextMaterial, err := json.Marshal(prepared.Action.ContextMaterial)
	if err != nil {
		return "", "", err
	}
	dependencies := make([]map[string]any, 0, len(prepared.Dependencies))
	for _, dependency := range prepared.Dependencies {
		dependencies = append(dependencies, map[string]any{
			"purpose": dependency.Purpose,
			"target": map[string]any{
				"id":             dependency.Target.ID,
				"project_id":     dependency.Target.ProjectID,
				"ref":            dependency.Target.Ref,
				"connector_kind": dependency.Target.ConnectorKind,
				"name":           dependency.Target.Name,
				"config":         dependency.Target.Config,
				"updated_at":     dependency.Target.UpdatedAt,
			},
			"profile": map[string]any{
				"id":              dependency.Profile.ID,
				"kind":            dependency.Profile.Kind,
				"label":           dependency.Profile.Label,
				"risk_label":      dependency.Profile.RiskLabel,
				"updated_at":      dependency.Profile.UpdatedAt,
				"secret_revision": dependency.Profile.SecretRevision,
				"public":          dependency.Profile.Public,
			},
		})
	}
	snapshot := map[string]any{
		"schema_version": approvalContextSchemaVersion,
		"captured_at":    capturedAt,
		"tool": map[string]any{
			"name":           connectorActionToolName,
			"schema_version": approvalContextSchemaVersion,
		},
		"connector": map[string]any{
			"kind":    prepared.Target.ConnectorKind,
			"version": prepared.ConnectorVersion,
		},
		"token": map[string]any{
			"id":         token.ID,
			"expires_at": token.ExpiresAt,
			"revoked_at": token.RevokedAt,
		},
		"permission": map[string]any{
			"rule":       permission.ExecutionRule,
			"expires_at": permission.ExpiresAt,
		},
		"project": map[string]any{
			"id":   permission.ProjectID,
			"name": permission.ProjectName,
			"slug": permission.ProjectSlug,
		},
		"target": map[string]any{
			"id":             prepared.Target.ID,
			"ref":            prepared.Target.Ref,
			"connector_kind": prepared.Target.ConnectorKind,
			"name":           prepared.Target.Name,
			"config":         prepared.Target.Config,
			"updated_at":     prepared.Target.UpdatedAt,
		},
		"profile": map[string]any{
			"id":              prepared.Profile.ID,
			"kind":            prepared.Profile.Kind,
			"label":           prepared.Profile.Label,
			"risk_label":      prepared.Profile.RiskLabel,
			"updated_at":      prepared.Profile.UpdatedAt,
			"secret_revision": prepared.Profile.SecretRevision,
			"public":          prepared.Profile.Public,
		},
		"action": map[string]any{
			"name":            prepared.Action.ActionName,
			"risk":            prepared.Action.Risk,
			"definition":      actionDefinition,
			"definition_hash": sha256Hex(string(actionDefinitionHashMaterial)),
			"payload_hash":    sha256Hex(string(payloadHashMaterial)),
			"context_hash":    sha256Hex(string(contextMaterial)),
		},
		"dependencies": dependencies,
	}
	hash, err := hashGenericApprovalContext(snapshot)
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", err
	}
	return string(payload), hash, nil
}

func hashGenericApprovalContext(snapshot map[string]any) (string, error) {
	clone := map[string]any{}
	for key, value := range snapshot {
		if key == "captured_at" {
			continue
		}
		clone[key] = value
	}
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(payload)), nil
}

func connectorApprovalDriftReason(previousContext string, currentContext string) string {
	var previous map[string]any
	var current map[string]any
	if err := json.Unmarshal([]byte(previousContext), &previous); err != nil {
		return "unknown"
	}
	if err := json.Unmarshal([]byte(currentContext), &current); err != nil {
		return "unknown"
	}
	for _, area := range []string{"connector", "token", "permission", "project", "target", "profile", "dependencies"} {
		if !reflect.DeepEqual(previous[area], current[area]) {
			return area
		}
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "definition_hash"), approvalActionValue(current, "definition_hash")) {
		return "action_definition"
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "payload_hash"), approvalActionValue(current, "payload_hash")) {
		return "payload"
	}
	if !reflect.DeepEqual(approvalActionValue(previous, "context_hash"), approvalActionValue(current, "context_hash")) {
		return "action_context"
	}
	if !reflect.DeepEqual(previous["action"], current["action"]) {
		return "action"
	}
	return "unknown"
}

func approvalActionValue(snapshot map[string]any, key string) any {
	action, _ := snapshot["action"].(map[string]any)
	if action == nil {
		return nil
	}
	return action[key]
}
