package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/projectcapabilities"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func validateVaultApprovalAuthorization(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	request vaultrequests.Request,
	approval vaultApprovalContext,
) (projectcapabilities.Capability, error) {
	if !runtime.isMCPStarted() {
		return projectcapabilities.Capability{}, staleVaultContext("MCP execution stopped; send a fresh request after it starts")
	}
	token, err := runtime.tokens.Get(ctx, request.TokenID)
	if err != nil || !tokens.Active(token.RevokedAt, token.ExpiresAt, time.Now().UTC()) ||
		token.ExpiresAt != approval.TokenExpiresAt || token.UpdatedAt != approval.TokenUpdatedAt {
		return projectcapabilities.Capability{}, staleVaultContext("Vault approval token changed; send a fresh request")
	}
	capability, err := projectcapabilities.NewStore(runtime.database).Effective(
		ctx, request.TokenID, request.ProjectID, approval.CapabilityName, time.Now(),
	)
	if err != nil || capability.ExecutionRule != approval.CapabilityExecutionRule ||
		!isExecutableVaultRule(approval.CapabilityExecutionRule) ||
		capability.Revision != approval.CapabilityRevision ||
		capability.ExpiresAt != approval.CapabilityExpiresAt {
		return projectcapabilities.Capability{}, staleVaultContext("Vault project capability changed; send a fresh request")
	}
	if err := requireVaultProjectVisibility(ctx, runtime, request.TokenID, approval.SourceProjectIDs); err != nil {
		return projectcapabilities.Capability{}, staleVaultContext("Vault source project visibility changed; send a fresh request")
	}
	scopeHash, err := currentVaultProjectScopeHash(ctx, runtime, request.TokenID, approval.SourceProjectIDs)
	if err != nil || scopeHash != approval.ProjectScopeHash {
		return projectcapabilities.Capability{}, staleVaultContext("Vault project scope changed; send a fresh request")
	}
	if request.ActionName != vaultrequests.ActionRestartSession {
		if approval.ExecutionRule != approval.CapabilityExecutionRule {
			return projectcapabilities.Capability{}, staleVaultContext("Vault execution rule changed; send a fresh request")
		}
		return capability, nil
	}
	permission, actionName, err := currentVaultLiveConsolePermission(
		ctx, server, runtime, request.TokenID, approval.TargetID, approval.ProfileID, approval.ConnectorKind,
	)
	if err != nil || actionName != approval.ConnectorActionName ||
		string(permission.ExecutionRule) != approval.ConnectorExecutionRule ||
		permission.ExpiresAt != approval.ConnectorPermissionExpiresAt ||
		permission.UpdatedAt != approval.ConnectorPermissionUpdatedAt ||
		effectiveVaultExecutionRule(capability.ExecutionRule, permission.ExecutionRule) != approval.ExecutionRule {
		return projectcapabilities.Capability{}, staleVaultContext("connector action permission changed; send a fresh request")
	}
	surface, err := connectortargets.NewStore(runtime.database).GetRuntimeSurface(ctx, approval.RuntimeID)
	if err != nil || surface.TargetID != approval.TargetID || surface.ProfileID != approval.ProfileID ||
		surface.ConnectorKind != approval.ConnectorKind ||
		surface.CapabilityKind != connectortargets.RuntimeCapabilityLiveConsole ||
		surface.UpdatedAt != approval.RuntimeSurfaceUpdatedAt {
		return projectcapabilities.Capability{}, staleVaultContext("connector runtime changed; send a fresh request")
	}
	version, err := sessionEnvironmentCapabilityVersion(ctx, server, runtime, approval.RuntimeID)
	if err != nil || version != approval.RuntimeCapabilityVersion {
		return projectcapabilities.Capability{}, staleVaultContext("connector Vault capability changed; send a fresh request")
	}
	targetHash, err := currentVaultTargetContextHash(ctx, runtime, approval.TargetID, approval.ProfileID)
	if err != nil || targetHash != approval.TargetContextHash {
		return projectcapabilities.Capability{}, staleVaultContext("target or credential profile changed; send a fresh request")
	}
	peers, err := expectedLiveConsolePeerIdentities(ctx, server, runtime, surface)
	if err != nil || !equalStrings(peers, approval.ExpectedPeerIdentities) {
		return projectcapabilities.Capability{}, staleVaultContext("connector peer trust changed; send a fresh request")
	}
	return capability, nil
}

func isExecutableVaultRule(rule string) bool {
	return rule == projectcapabilities.RuleApprovalRequired || rule == projectcapabilities.RuleAlwaysRun
}

func currentVaultLiveConsolePermission(
	ctx context.Context,
	server *Server,
	runtime *databaseRuntime,
	tokenID int64,
	targetID int64,
	profileID int64,
	connectorKind string,
) (connectortargets.ActionPermission, string, error) {
	liveConsole, ok := server.connectorAPIAdapterFor(connectorKind).(connectorapi.LiveConsoleAdapter)
	if !ok {
		return connectortargets.ActionPermission{}, "", errors.New("this connector does not expose a live console action")
	}
	actionName := strings.TrimSpace(liveConsole.LiveConsoleActionName())
	if actionName == "" {
		return connectortargets.ActionPermission{}, "", errors.New("this connector has an invalid live console action")
	}
	permission, err := connectortargets.NewStore(runtime.database).GetActionPermission(
		ctx, tokenID, targetID, profileID, actionName, time.Now().UTC(),
	)
	if err != nil || (permission.ExecutionRule != connectortargets.ActionPermissionAlwaysRun &&
		permission.ExecutionRule != connectortargets.ActionPermissionApprovalRequired) {
		return connectortargets.ActionPermission{}, "", errors.New("Vault session apply requires an active Prompt or Always connector action permission")
	}
	return permission, actionName, nil
}

func effectiveVaultExecutionRule(capabilityRule string, connectorRule connectortargets.ActionPermissionRule) string {
	if capabilityRule == projectcapabilities.RuleAlwaysRun &&
		connectorRule == connectortargets.ActionPermissionAlwaysRun {
		return projectcapabilities.RuleAlwaysRun
	}
	return projectcapabilities.RuleApprovalRequired
}
