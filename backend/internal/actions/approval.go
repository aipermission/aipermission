package actions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

const approvalContextSchemaVersion = "connector-action-v2"

// ApprovalTokenSnapshot contains the non-secret token state that authorizes an
// action. Names and other presentation-only fields deliberately do not affect
// approval drift.
type ApprovalTokenSnapshot struct {
	ID        int64
	ExpiresAt string
	RevokedAt string
}

// ApprovalPermissionSnapshot contains the permission and project identity
// captured at approval time.
type ApprovalPermissionSnapshot struct {
	Rule        string
	ExpiresAt   string
	ProjectID   int64
	ProjectName string
	ProjectSlug string
}

// BuildApprovalContext creates the canonical approval snapshot and its stable
// hash. CapturedAt is retained for display but excluded from the drift hash.
func BuildApprovalContext(prepared PreparedRequest, token ApprovalTokenSnapshot, permission ApprovalPermissionSnapshot, capturedAt string) (string, string, error) {
	payloadHashMaterial, err := json.Marshal(map[string]any{
		"input": prepared.Requested.Input, "payload": prepared.Action.Payload,
	})
	if err != nil {
		return "", "", err
	}
	actionDefinition := map[string]any{
		"name": prepared.ActionDefinition.Name, "label": prepared.ActionDefinition.Label,
		"description": prepared.ActionDefinition.Description, "category": prepared.ActionDefinition.Category,
		"risk": prepared.ActionDefinition.Risk, "retry_policy": connectors.EffectiveRetryPolicy(prepared.ActionDefinition),
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
				"id": dependency.Target.ID, "project_id": dependency.Target.ProjectID,
				"ref": dependency.Target.Ref, "connector_kind": dependency.Target.ConnectorKind,
				"name": dependency.Target.Name, "config": dependency.Target.Config,
				"updated_at": dependency.Target.UpdatedAt,
			},
			"profile": map[string]any{
				"id": dependency.Profile.ID, "kind": dependency.Profile.Kind,
				"label": dependency.Profile.Label, "risk_label": dependency.Profile.RiskLabel,
				"updated_at": dependency.Profile.UpdatedAt, "secret_revision": dependency.Profile.SecretRevision,
				"public": dependency.Profile.Public,
			},
		})
	}
	snapshot := map[string]any{
		"schema_version": approvalContextSchemaVersion,
		"captured_at":    capturedAt,
		"tool": map[string]any{
			"name": "connector.call_action", "schema_version": approvalContextSchemaVersion,
		},
		"connector":  map[string]any{"kind": prepared.Target.ConnectorKind, "version": prepared.ConnectorVersion},
		"token":      map[string]any{"id": token.ID, "expires_at": token.ExpiresAt, "revoked_at": token.RevokedAt},
		"permission": map[string]any{"rule": permission.Rule, "expires_at": permission.ExpiresAt},
		"project":    map[string]any{"id": permission.ProjectID, "name": permission.ProjectName, "slug": permission.ProjectSlug},
		"target": map[string]any{
			"id": prepared.Target.ID, "ref": prepared.Target.Ref,
			"connector_kind": prepared.Target.ConnectorKind, "name": prepared.Target.Name,
			"config": prepared.Target.Config, "updated_at": prepared.Target.UpdatedAt,
		},
		"profile": map[string]any{
			"id": prepared.Profile.ID, "kind": prepared.Profile.Kind, "label": prepared.Profile.Label,
			"risk_label": prepared.Profile.RiskLabel, "updated_at": prepared.Profile.UpdatedAt,
			"secret_revision": prepared.Profile.SecretRevision, "public": prepared.Profile.Public,
		},
		"action": map[string]any{
			"name": prepared.Action.ActionName, "risk": prepared.Action.Risk,
			"definition": actionDefinition, "definition_hash": approvalSHA256(actionDefinitionHashMaterial),
			"payload_hash": approvalSHA256(payloadHashMaterial), "context_hash": approvalSHA256(contextMaterial),
		},
		"dependencies": dependencies,
	}
	hash, err := hashApprovalContext(snapshot)
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return "", "", err
	}
	return string(payload), hash, nil
}

// ApprovalDriftReason identifies the first security-relevant snapshot area
// that changed between approval and dispatch.
func ApprovalDriftReason(previousContext string, currentContext string) string {
	var previous map[string]any
	var current map[string]any
	if json.Unmarshal([]byte(previousContext), &previous) != nil || json.Unmarshal([]byte(currentContext), &current) != nil {
		return "unknown"
	}
	for _, area := range []string{"connector", "token", "permission", "project", "target", "profile", "dependencies"} {
		if !reflect.DeepEqual(previous[area], current[area]) {
			return area
		}
	}
	for _, field := range []struct{ name, reason string }{
		{"definition_hash", "action_definition"}, {"payload_hash", "payload"}, {"context_hash", "action_context"},
	} {
		if !reflect.DeepEqual(approvalActionValue(previous, field.name), approvalActionValue(current, field.name)) {
			return field.reason
		}
	}
	if !reflect.DeepEqual(previous["action"], current["action"]) {
		return "action"
	}
	return "unknown"
}

func hashApprovalContext(snapshot map[string]any) (string, error) {
	clone := map[string]any{}
	for key, value := range snapshot {
		if key != "captured_at" {
			clone[key] = value
		}
	}
	payload, err := json.Marshal(clone)
	if err != nil {
		return "", err
	}
	return approvalSHA256(payload), nil
}

func approvalSHA256(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func approvalActionValue(snapshot map[string]any, key string) any {
	action, _ := snapshot["action"].(map[string]any)
	return action[key]
}
