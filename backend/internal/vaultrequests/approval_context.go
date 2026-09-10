package vaultrequests

import "github.com/aipermission/aipermission/backend/internal/projectvault"

const ApprovalContextSchema = "vault-action-v3"

type ApprovalContext struct {
	Schema                       string                     `json:"schema"`
	ActionName                   string                     `json:"action_name"`
	TokenID                      int64                      `json:"token_id"`
	ProjectID                    int64                      `json:"project_id"`
	WorkspaceID                  string                     `json:"workspace_id"`
	RuntimeInstanceID            string                     `json:"runtime_instance_id"`
	CapabilityName               string                     `json:"capability_name"`
	ExecutionRule                string                     `json:"execution_rule"`
	CapabilityExecutionRule      string                     `json:"capability_execution_rule"`
	CapabilityRevision           int64                      `json:"capability_revision"`
	CapabilityExpiresAt          string                     `json:"capability_expires_at,omitempty"`
	TokenExpiresAt               string                     `json:"token_expires_at,omitempty"`
	TokenUpdatedAt               string                     `json:"token_updated_at"`
	InputHash                    string                     `json:"input_hash"`
	RuntimeID                    int64                      `json:"runtime_id,omitempty"`
	RuntimeSurfaceUpdatedAt      string                     `json:"runtime_surface_updated_at,omitempty"`
	RuntimeCapabilityVersion     string                     `json:"runtime_capability_version,omitempty"`
	TargetID                     int64                      `json:"target_id,omitempty"`
	ProfileID                    int64                      `json:"profile_id,omitempty"`
	ConnectorKind                string                     `json:"connector_kind,omitempty"`
	ConnectorActionName          string                     `json:"connector_action_name,omitempty"`
	ConnectorExecutionRule       string                     `json:"connector_execution_rule,omitempty"`
	ConnectorPermissionExpiresAt string                     `json:"connector_permission_expires_at,omitempty"`
	ConnectorPermissionUpdatedAt string                     `json:"connector_permission_updated_at,omitempty"`
	TargetContextHash            string                     `json:"target_context_hash,omitempty"`
	ExpectedPeerIdentities       []string                   `json:"expected_peer_identities,omitempty"`
	ExpectedSessionID            int64                      `json:"expected_session_id,omitempty"`
	ExpectedGeneration           int64                      `json:"expected_session_generation,omitempty"`
	ExpectedCols                 int                        `json:"expected_cols,omitempty"`
	ExpectedRows                 int                        `json:"expected_rows,omitempty"`
	EnvironmentContentHash       string                     `json:"environment_content_hash,omitempty"`
	Items                        []projectvault.SessionItem `json:"items,omitempty"`
	SourceProjectIDs             []int64                    `json:"source_project_ids,omitempty"`
	ProjectScopeHash             string                     `json:"project_scope_hash"`
}
