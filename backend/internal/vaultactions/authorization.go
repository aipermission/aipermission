package vaultactions

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func (r *Runtime) validateAuthorization(
	ctx context.Context,
	request vaultrequests.Request,
	approval vaultrequests.ApprovalContext,
) (accesscontrol.Capability, error) {
	if err := r.validate(); err != nil {
		return accesscontrol.Capability{}, err
	}
	if !r.mcpStarted() {
		return accesscontrol.Capability{}, staleContext("MCP execution stopped; send a fresh request after it starts")
	}
	capability, err := r.validateCapabilityAuthorization(ctx, request, approval)
	if err != nil {
		return accesscontrol.Capability{}, err
	}
	if request.ActionName != vaultrequests.ActionRestartSession {
		if approval.ExecutionRule != approval.CapabilityExecutionRule {
			return accesscontrol.Capability{}, staleContext("Vault execution rule changed; send a fresh request")
		}
		return capability, nil
	}
	if err := r.validateRestartAuthorization(ctx, request.TokenID, approval, capability); err != nil {
		return accesscontrol.Capability{}, err
	}
	return capability, nil
}

func (r *Runtime) validateCapabilityAuthorization(
	ctx context.Context,
	request vaultrequests.Request,
	approval vaultrequests.ApprovalContext,
) (accesscontrol.Capability, error) {
	token, err := r.tokens.Get(ctx, request.TokenID)
	if err != nil {
		return accesscontrol.Capability{}, err
	}
	if !token.Active || token.ExpiresAt != approval.TokenExpiresAt || token.UpdatedAt != approval.TokenUpdatedAt {
		return accesscontrol.Capability{}, staleContext("Vault approval token changed; send a fresh request")
	}
	capability, err := accesscontrol.NewCapabilityStore(r.database).Effective(
		ctx, request.TokenID, request.ProjectID, approval.CapabilityName, time.Now(),
	)
	if errors.Is(err, accesscontrol.ErrCapabilityNotFound) {
		return accesscontrol.Capability{}, staleContext("Vault project capability changed; send a fresh request")
	}
	if err != nil {
		return accesscontrol.Capability{}, err
	}
	if capability.ExecutionRule != approval.CapabilityExecutionRule || !isExecutableRule(approval.CapabilityExecutionRule) || capability.Revision != approval.CapabilityRevision ||
		capability.ExpiresAt != approval.CapabilityExpiresAt {
		return accesscontrol.Capability{}, staleContext("Vault project capability changed; send a fresh request")
	}
	if err := r.requireProjectVisibility(ctx, request.TokenID, approval.SourceProjectIDs); err != nil {
		if !errors.Is(err, errProjectVisibilityDenied) {
			return accesscontrol.Capability{}, err
		}
		return accesscontrol.Capability{}, staleContext("Vault source project visibility changed; send a fresh request")
	}
	scopeHash, err := r.projectScopeHash(ctx, request.TokenID, approval.SourceProjectIDs)
	if errors.Is(err, sql.ErrNoRows) {
		return accesscontrol.Capability{}, staleContext("Vault project scope changed; send a fresh request")
	}
	if err != nil {
		return accesscontrol.Capability{}, err
	}
	if scopeHash != approval.ProjectScopeHash {
		return accesscontrol.Capability{}, staleContext("Vault project scope changed; send a fresh request")
	}
	return capability, nil
}

func (r *Runtime) validateRestartAuthorization(
	ctx context.Context,
	tokenID int64,
	approval vaultrequests.ApprovalContext,
	capability accesscontrol.Capability,
) error {
	permission, actionName, err := r.connector.LiveConsolePermission(
		ctx, tokenID, approval.TargetID, approval.ProfileID, approval.ConnectorKind,
	)
	if err != nil {
		return err
	}
	if actionName != approval.ConnectorActionName ||
		string(permission.ExecutionRule) != approval.ConnectorExecutionRule ||
		permission.ExpiresAt != approval.ConnectorPermissionExpiresAt ||
		permission.UpdatedAt != approval.ConnectorPermissionUpdatedAt ||
		effectiveExecutionRule(capability.ExecutionRule, permission.ExecutionRule) != approval.ExecutionRule {
		return staleContext("connector action permission changed; send a fresh request")
	}
	surface, err := connectortargets.NewStore(r.database).GetRuntimeSurface(ctx, approval.RuntimeID)
	if errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		return staleContext("connector runtime changed; send a fresh request")
	}
	if err != nil {
		return err
	}
	if surface.TargetID != approval.TargetID || surface.ProfileID != approval.ProfileID ||
		surface.ConnectorKind != approval.ConnectorKind ||
		surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole ||
		surface.UpdatedAt != approval.RuntimeSurfaceUpdatedAt {
		return staleContext("connector runtime changed; send a fresh request")
	}
	version, err := r.connector.SessionEnvironmentVersion(ctx, approval.RuntimeID)
	if err != nil {
		return err
	}
	if version != approval.RuntimeCapabilityVersion {
		return staleContext("connector Vault capability changed; send a fresh request")
	}
	targetHash, err := r.targetContextHash(ctx, approval.TargetID, approval.ProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return staleContext("target or credential profile changed; send a fresh request")
	}
	if err != nil {
		return err
	}
	if targetHash != approval.TargetContextHash {
		return staleContext("target or credential profile changed; send a fresh request")
	}
	peerExpectation, err := r.connector.ExpectedPeerIdentities(ctx, surface)
	peers := normalizeIdentities(peerExpectation.Items)
	if err != nil {
		return err
	}
	if (peerExpectation.Required && len(peers) == 0) ||
		!equalStrings(peers, approval.ExpectedPeerIdentities) {
		return staleContext("connector peer trust changed; send a fresh request")
	}
	return nil
}

func (r *Runtime) ValidateAuthorization(
	ctx context.Context,
	request vaultrequests.Request,
	approval vaultrequests.ApprovalContext,
) error {
	_, err := r.validateAuthorization(ctx, request, approval)
	return err
}

func (r *Runtime) AuthorizeOutput(ctx context.Context, request vaultrequests.Request) vaultrequests.OutputAuthorization {
	if !r.mcpStarted() {
		return vaultrequests.OutputWithheld
	}
	approval, err := vaultrequests.DecodeApprovalContext(request.ApprovalContext)
	if err != nil || approval.TokenID != request.TokenID || approval.ProjectID != request.ProjectID {
		return vaultrequests.OutputContextStale
	}
	if _, err := r.validateAuthorization(ctx, request, approval); err != nil {
		if IsStale(err) {
			return vaultrequests.OutputContextStale
		}
		return vaultrequests.OutputWithheld
	}
	if request.ActionName != vaultrequests.ActionRestartSession || request.Status != vaultrequests.StatusCompleted {
		return vaultrequests.OutputAuthorized
	}
	output, ok := request.Output.(map[string]any)
	if !ok {
		return vaultrequests.OutputWithheld
	}
	sessionID := jsonInt(output["session_id"])
	generation := jsonInt(output["session_generation"])
	runtimeID := jsonInt(output["runtime_id"])
	if sessionID < 1 || generation < 1 || runtimeID != approval.RuntimeID {
		return vaultrequests.OutputWithheld
	}
	principal, err := r.TokenPrincipal(request.TokenID)
	if err != nil {
		return vaultrequests.OutputWithheld
	}
	if !vaultsessions.NewObserver(r.database, r.leases).Authorized(ctx, principal, vaultsessions.ObserveRequest{
		SessionID: sessionID, SessionGeneration: generation,
		ExpectedRuntimeID: runtimeID, RequireEnvironment: true,
	}) {
		return vaultrequests.OutputWithheld
	}
	return vaultrequests.OutputAuthorized
}

func jsonInt(value any) int64 {
	switch typed := value.(type) {
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
