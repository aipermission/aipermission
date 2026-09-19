package vaultactions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func (r *Runtime) buildApprovalContext(
	ctx context.Context,
	tokenID int64,
	project Project,
	actionName string,
	input map[string]any,
) (vaultrequests.ApprovalContext, string, map[string]any, error) {
	if actionName != vaultrequests.ActionGenerateItem && actionName != vaultrequests.ActionRestartSession {
		return vaultrequests.ApprovalContext{}, "", nil, errors.New("unsupported Vault action")
	}
	normalizedInput, err := vaultrequests.NormalizeActionInput(actionName, input)
	if err != nil {
		return vaultrequests.ApprovalContext{}, "", nil, err
	}
	capabilityName := accesscontrol.VaultItemGenerate
	if actionName == vaultrequests.ActionRestartSession {
		capabilityName = accesscontrol.VaultSessionApply
	}
	capability, err := accesscontrol.NewCapabilityStore(r.database).Effective(ctx, tokenID, project.ID, capabilityName, time.Now())
	if err != nil {
		return vaultrequests.ApprovalContext{}, "", nil, err
	}
	if !isExecutableRule(capability.ExecutionRule) {
		return vaultrequests.ApprovalContext{}, "", nil, errors.New("this Vault action requires an active Prompt or Always project capability")
	}
	approval := vaultrequests.ApprovalContext{
		Schema: vaultrequests.ApprovalContextSchema, ActionName: actionName, TokenID: tokenID,
		ProjectID: project.ID, WorkspaceID: r.workspaceID, RuntimeInstanceID: r.runtimeInstanceID,
		CapabilityName: capabilityName, ExecutionRule: capability.ExecutionRule,
		CapabilityExecutionRule: capability.ExecutionRule, CapabilityRevision: capability.Revision,
		CapabilityExpiresAt: capability.ExpiresAt,
	}
	token, err := r.tokens.Get(ctx, tokenID)
	if err != nil {
		return vaultrequests.ApprovalContext{}, "", nil, err
	}
	approval.TokenExpiresAt = token.ExpiresAt
	approval.TokenUpdatedAt = token.UpdatedAt
	approval.InputHash, err = hashCanonical(normalizedInput)
	if err != nil {
		return vaultrequests.ApprovalContext{}, "", nil, err
	}
	if actionName == vaultrequests.ActionGenerateItem {
		if err := r.completeGenerateApproval(ctx, tokenID, project.ID, normalizedInput, &approval); err != nil {
			return vaultrequests.ApprovalContext{}, "", nil, err
		}
	}
	if actionName == vaultrequests.ActionRestartSession {
		if err := r.completeSessionApproval(ctx, tokenID, project.ID, normalizedInput, capability, &approval); err != nil {
			return vaultrequests.ApprovalContext{}, "", nil, err
		}
	}
	approval.ProjectScopeHash, err = r.projectScopeHash(ctx, tokenID, approval.SourceProjectIDs)
	if err != nil {
		return vaultrequests.ApprovalContext{}, "", nil, err
	}
	hash, err := hashCanonical(approval)
	return approval, hash, normalizedInput, err
}

func (r *Runtime) completeGenerateApproval(
	ctx context.Context,
	tokenID, projectID int64,
	input map[string]any,
	approval *vaultrequests.ApprovalContext,
) error {
	actionInput, err := vaultrequests.DecodeGenerateInput(input)
	if err != nil {
		return err
	}
	normalized, err := projectvault.NormalizeCreateMetadata(generateCreateInput(projectID, actionInput))
	if err != nil {
		return err
	}
	approval.SourceProjectIDs = append([]int64{projectID}, normalized.SharedProjectIDs...)
	return r.requireProjectVisibility(ctx, tokenID, approval.SourceProjectIDs)
}

func (r *Runtime) completeSessionApproval(
	ctx context.Context,
	tokenID, projectID int64,
	input map[string]any,
	capability accesscontrol.Capability,
	approval *vaultrequests.ApprovalContext,
) error {
	actionInput, err := vaultrequests.DecodeSessionApplyInput(input)
	if err != nil || strings.TrimSpace(actionInput.TargetRef) == "" || len(actionInput.Items) == 0 {
		return errors.New("target_ref and at least one Vault item are required")
	}
	targets := connectortargets.NewStore(r.database)
	target, profile, err := targets.ResolveConnectorActionTarget(ctx, actionInput.TargetRef)
	if err != nil {
		return err
	}
	if target.ProjectID != projectID {
		return errors.New("target_ref must belong to the selected project")
	}
	surface, err := targets.GetRuntimeSurfaceByProfile(ctx, target.ConnectorKind, target.ID, profile.ID, connectortargets.RuntimeCapabilityLiveConsole)
	if err != nil {
		return err
	}
	capabilityVersion, err := r.connector.SessionEnvironmentVersion(ctx, surface.ID)
	if err != nil {
		return err
	}
	snapshot, err := r.buildEnvironmentSnapshot(ctx, surface.ID, actionInput.SessionSelections())
	if err != nil {
		return err
	}
	approval.Items = append([]projectvault.SessionItem(nil), snapshot.Items...)
	approval.SourceProjectIDs = uniquePositiveIDs(append(sessionProjectIDs(snapshot.Items), projectID))
	if err := r.requireProjectVisibility(ctx, tokenID, approval.SourceProjectIDs); err != nil {
		return err
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
	permission, connectorAction, err := r.connector.LiveConsolePermission(ctx, tokenID, target.ID, profile.ID, target.ConnectorKind)
	if err != nil {
		return err
	}
	approval.ConnectorActionName = connectorAction
	approval.ConnectorExecutionRule = string(permission.ExecutionRule)
	approval.ConnectorPermissionExpiresAt = permission.ExpiresAt
	approval.ConnectorPermissionUpdatedAt = permission.UpdatedAt
	approval.ExecutionRule = effectiveExecutionRule(capability.ExecutionRule, permission.ExecutionRule)
	record, activeErr := r.sessions.ActiveRecord(ctx, surface.ID)
	if activeErr == nil {
		approval.ExpectedSessionID = record.ID
		approval.ExpectedGeneration = record.Generation
		approval.ExpectedCols = record.Cols
		approval.ExpectedRows = record.Rows
	} else if !errors.Is(activeErr, console.ErrNotFound) {
		return activeErr
	}
	return nil
}

func (r *Runtime) targetContextHash(ctx context.Context, targetID, profileID int64) (string, error) {
	var targetUpdatedAt, profileUpdatedAt, encryptedProfileSecret string
	err := r.database.QueryRowContext(ctx, `
		SELECT t.updated_at, p.updated_at, p.encrypted_secret_json
		FROM connector_targets t
		JOIN connector_credential_profiles p ON p.target_id = t.id AND p.connector_kind = t.connector_kind
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

func (r *Runtime) requireProjectVisibility(ctx context.Context, tokenID int64, projectIDs []int64) error {
	for _, projectID := range uniquePositiveIDs(projectIDs) {
		allowed, err := r.projects.TokenCanAccess(ctx, tokenID, projectID)
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("token cannot access one or more Vault source projects")
		}
	}
	return nil
}

func (r *Runtime) projectScopeHash(ctx context.Context, tokenID int64, projectIDs []int64) (string, error) {
	type scopeRevision struct {
		ProjectID      int64  `json:"project_id"`
		ProjectStatus  string `json:"project_status"`
		ProjectUpdated string `json:"project_updated_at"`
		Enabled        int    `json:"enabled"`
		ScopeUpdated   string `json:"scope_updated_at"`
	}
	revisions := make([]scopeRevision, 0, len(projectIDs))
	for _, projectID := range uniquePositiveIDs(projectIDs) {
		item := scopeRevision{ProjectID: projectID}
		err := r.database.QueryRowContext(ctx, `
			SELECT p.status, p.updated_at, COALESCE(s.enabled, 0), COALESCE(s.updated_at, '')
			FROM projects p
			LEFT JOIN token_project_scopes s ON s.project_id = p.id AND s.token_id = ?
			WHERE p.id = ?`, tokenID, projectID,
		).Scan(&item.ProjectStatus, &item.ProjectUpdated, &item.Enabled, &item.ScopeUpdated)
		if err != nil {
			return "", err
		}
		revisions = append(revisions, item)
	}
	return hashCanonical(revisions)
}

func sessionProjectIDs(items []projectvault.SessionItem) []int64 {
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

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func isExecutableRule(rule string) bool {
	return rule == accesscontrol.RuleApprovalRequired || rule == accesscontrol.RuleAlwaysRun
}

func effectiveExecutionRule(capabilityRule string, connectorRule connectortargets.ActionPermissionRule) string {
	if capabilityRule == accesscontrol.RuleAlwaysRun && connectorRule == connectortargets.ActionPermissionAlwaysRun {
		return accesscontrol.RuleAlwaysRun
	}
	return accesscontrol.RuleApprovalRequired
}
