package api

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

const vaultApprovalContextSchema = "vault-action-v3"

type vaultApprovalContext struct {
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

type vaultContextDriftError struct{ message string }

func (e vaultContextDriftError) Error() string { return e.message }

func staleVaultContext(message string) error {
	return vaultContextDriftError{message: message}
}

func isVaultContextDrift(err error) bool {
	var target vaultContextDriftError
	return errors.As(err, &target)
}

func resolveProjectRef(ctx context.Context, runtime *databaseRuntime, ref string) (projectstore.Project, error) {
	ref = strings.TrimSpace(ref)
	if id, err := strconv.ParseInt(ref, 10, 64); err == nil && id > 0 {
		return projectstore.NewStore(runtime.database).Get(ctx, id)
	}
	items, err := projectstore.NewStore(runtime.database).List(ctx)
	if err != nil {
		return projectstore.Project{}, err
	}
	for _, item := range items {
		if item.Slug == ref {
			return item, nil
		}
	}
	return projectstore.Project{}, projectstore.ErrNotFound
}

func buildVaultApprovalContext(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	tokenID int64,
	project projectstore.Project,
	actionName string,
	input map[string]any,
) (vaultApprovalContext, string, map[string]any, error) {
	if actionName != vaultrequests.ActionGenerateItem && actionName != vaultrequests.ActionRestartSession {
		return vaultApprovalContext{}, "", nil, errors.New("unsupported Vault action")
	}
	normalizedInput, err := normalizeVaultActionInput(actionName, input)
	if err != nil {
		return vaultApprovalContext{}, "", nil, err
	}
	capabilityName := projectcapabilities.VaultItemGenerate
	if actionName == vaultrequests.ActionRestartSession {
		capabilityName = projectcapabilities.VaultSessionApply
	}
	capability, err := projectcapabilities.NewStore(runtime.database).Effective(ctx, tokenID, project.ID, capabilityName, time.Now())
	if err != nil {
		return vaultApprovalContext{}, "", nil, err
	}
	if !isExecutableVaultRule(capability.ExecutionRule) {
		return vaultApprovalContext{}, "", nil, errors.New("this Vault action requires an active Prompt or Always project capability")
	}
	approval := vaultApprovalContext{
		Schema: vaultApprovalContextSchema, ActionName: actionName, TokenID: tokenID,
		ProjectID: project.ID, WorkspaceID: runtime.workspaceUUID,
		RuntimeInstanceID: runtime.runtimeInstanceID, CapabilityName: capabilityName,
		ExecutionRule:           capability.ExecutionRule,
		CapabilityExecutionRule: capability.ExecutionRule,
		CapabilityRevision:      capability.Revision, CapabilityExpiresAt: capability.ExpiresAt,
	}
	token, err := runtime.tokens.Get(ctx, tokenID)
	if err != nil {
		return vaultApprovalContext{}, "", nil, err
	}
	approval.TokenExpiresAt = token.ExpiresAt
	approval.TokenUpdatedAt = token.UpdatedAt
	approval.InputHash, err = hashCanonical(normalizedInput)
	if err != nil {
		return vaultApprovalContext{}, "", nil, err
	}
	if actionName == vaultrequests.ActionGenerateItem {
		var actionInput vaultGenerateActionInput
		if err := decodeMap(normalizedInput, &actionInput); err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		if err := projectvault.ValidateSessionItemName(actionInput.Name); err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		if err := projectvault.ValidateGeneratorKind(actionInput.GeneratorKind); err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		approval.SourceProjectIDs = uniquePositiveIDs(append([]int64{project.ID}, actionInput.SharedProjectIDs...))
		if err := requireVaultProjectVisibility(ctx, runtime, tokenID, approval.SourceProjectIDs); err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
	}
	if actionName == vaultrequests.ActionRestartSession {
		var actionInput vaultSessionApplyActionInput
		if err := decodeMap(normalizedInput, &actionInput); err != nil || strings.TrimSpace(actionInput.TargetRef) == "" || len(actionInput.Items) == 0 {
			return vaultApprovalContext{}, "", nil, errors.New("target_ref and at least one Vault item are required")
		}
		targets := connectortargets.NewStore(runtime.database)
		target, profile, err := targets.ResolveConnectorActionTarget(ctx, actionInput.TargetRef)
		if err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		var targetProjectID int64
		if err := runtime.database.QueryRowContext(ctx, `SELECT project_id FROM connector_targets WHERE id = ? AND status = 'active'`, target.ID).Scan(&targetProjectID); err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		if targetProjectID != project.ID {
			return vaultApprovalContext{}, "", nil, errors.New("target_ref must belong to the selected project")
		}
		surface, err := targets.GetRuntimeSurfaceByProfile(ctx, target.ConnectorKind, target.ID, profile.ID, connectortargets.RuntimeCapabilityLiveConsole)
		if err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		capabilityVersion, err := sessionEnvironmentCapabilityVersion(ctx, server, runtime, surface.ID)
		if err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		snapshot, err := buildVaultEnvironmentSnapshot(ctx, server, runtime, surface.ID, actionInput.sessionSelections())
		if err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		approval.Items = append([]projectvault.SessionItem(nil), snapshot.Items...)
		approval.SourceProjectIDs = uniquePositiveIDs(append(uniqueSessionProjectIDs(snapshot.Items), project.ID))
		if err := requireVaultProjectVisibility(ctx, runtime, tokenID, approval.SourceProjectIDs); err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		approval.EnvironmentContentHash = snapshot.EnvironmentContentHash
		approval.RuntimeID = snapshot.RuntimeID
		approval.RuntimeSurfaceUpdatedAt = surface.UpdatedAt
		approval.RuntimeCapabilityVersion = capabilityVersion
		approval.TargetID = snapshot.TargetID
		approval.ProfileID = snapshot.ProfileID
		approval.ConnectorKind = snapshot.ConnectorKind
		approval.TargetContextHash = snapshot.TargetContextHash
		approval.ExpectedPeerIdentities = append([]string(nil), snapshot.PeerIdentities...)
		permission, actionName, err := currentVaultLiveConsolePermission(ctx, runtime, tokenID, target.ID, profile.ID, target.ConnectorKind)
		if err != nil {
			return vaultApprovalContext{}, "", nil, err
		}
		approval.ConnectorActionName = actionName
		approval.ConnectorExecutionRule = string(permission.ExecutionRule)
		approval.ConnectorPermissionExpiresAt = permission.ExpiresAt
		approval.ConnectorPermissionUpdatedAt = permission.UpdatedAt
		approval.ExecutionRule = effectiveVaultExecutionRule(capability.ExecutionRule, permission.ExecutionRule)
		record, activeErr := activeConsoleRecord(ctx, runtime, surface.ID)
		if activeErr == nil {
			approval.ExpectedSessionID = record.ID
			approval.ExpectedGeneration = record.Generation
			approval.ExpectedCols = record.Cols
			approval.ExpectedRows = record.Rows
		} else if !errors.Is(activeErr, console.ErrNotFound) {
			return vaultApprovalContext{}, "", nil, activeErr
		}
	}
	approval.ProjectScopeHash, err = currentVaultProjectScopeHash(ctx, runtime, tokenID, approval.SourceProjectIDs)
	if err != nil {
		return vaultApprovalContext{}, "", nil, err
	}
	hash, err := hashCanonical(approval)
	return approval, hash, normalizedInput, err
}

func currentVaultTargetContextHash(ctx context.Context, runtime *databaseRuntime, targetID, profileID int64) (string, error) {
	var targetUpdatedAt, profileUpdatedAt string
	var encryptedProfileSecret string
	err := runtime.database.QueryRowContext(ctx, `
		SELECT t.updated_at, p.updated_at, p.encrypted_secret_json
		FROM connector_targets t
		JOIN connector_credential_profiles p
		  ON p.target_id = t.id AND p.connector_kind = t.connector_kind
		WHERE t.id = ? AND p.id = ? AND t.status = 'active' AND p.status = 'active'`,
		targetID, profileID,
	).Scan(&targetUpdatedAt, &profileUpdatedAt, &encryptedProfileSecret)
	if err != nil {
		return "", err
	}
	profileSecretRevision := ""
	if strings.TrimSpace(encryptedProfileSecret) != "" {
		profileSecretRevision = sha256Hex(encryptedProfileSecret)
	}
	return hashCanonical(map[string]any{
		"target_id": targetID, "target_updated_at": targetUpdatedAt,
		"profile_id": profileID, "profile_updated_at": profileUpdatedAt,
		"profile_secret_revision": profileSecretRevision,
	})
}

func requireVaultProjectVisibility(ctx context.Context, runtime *databaseRuntime, tokenID int64, projectIDs []int64) error {
	store := projectstore.NewStore(runtime.database)
	seen := map[int64]bool{}
	for _, projectID := range projectIDs {
		if projectID < 1 || seen[projectID] {
			continue
		}
		seen[projectID] = true
		allowed, err := store.TokenCanAccessProject(ctx, tokenID, projectID)
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("token cannot access one or more Vault source projects")
		}
	}
	return nil
}

func currentVaultProjectScopeHash(ctx context.Context, runtime *databaseRuntime, tokenID int64, projectIDs []int64) (string, error) {
	type scopeRevision struct {
		ProjectID      int64  `json:"project_id"`
		ProjectStatus  string `json:"project_status"`
		ProjectUpdated string `json:"project_updated_at"`
		Enabled        int    `json:"enabled"`
		ScopeUpdated   string `json:"scope_updated_at"`
	}
	ids := uniquePositiveIDs(projectIDs)
	revisions := make([]scopeRevision, 0, len(ids))
	for _, projectID := range ids {
		item := scopeRevision{ProjectID: projectID}
		err := runtime.database.QueryRowContext(ctx, `
			SELECT p.status, p.updated_at, COALESCE(s.enabled, 0), COALESCE(s.updated_at, '')
			FROM projects p
			LEFT JOIN token_project_scopes s ON s.project_id = p.id AND s.token_id = ?
			WHERE p.id = ?`,
			tokenID, projectID,
		).Scan(&item.ProjectStatus, &item.ProjectUpdated, &item.Enabled, &item.ScopeUpdated)
		if err != nil {
			return "", err
		}
		revisions = append(revisions, item)
	}
	return hashCanonical(revisions)
}

func uniqueSessionProjectIDs(items []projectvault.SessionItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.SourceProjectID)
	}
	return uniquePositiveIDs(ids)
}

func uniquePositiveIDs(values []int64) []int64 {
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		if value > 0 && !seen[value] {
			seen[value] = true
			ids = append(ids, value)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func hashCanonical(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256Hex(string(payload)), nil
}

func sessionItemNames(items []projectvault.SessionItem) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}
