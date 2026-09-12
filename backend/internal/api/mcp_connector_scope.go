package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s mcpHandlers) mcpConnectorReadScope(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayaccess.MCPScope{}, false
	}
	scope, valid := s.accessOwner.MCPReadScope(auth.runtime, gatewayinfra.MCPReadPorts{
		TokenID: auth.TokenID,
		Permissions: func(ctx context.Context) ([]gatewayaccess.MCPPermission, error) {
			permissions, err := s.connectorCatalog(auth.runtime).ProjectScopedSupportedConnectorPermissions(ctx, auth.TokenID)
			if err != nil {
				return nil, err
			}
			result := make([]gatewayaccess.MCPPermission, 0, len(permissions))
			for _, permission := range permissions {
				result = append(result, gatewayaccess.MCPPermission{
					ProjectID: permission.ProjectID, ProjectName: permission.ProjectName, ProjectSlug: permission.ProjectSlug,
					TargetID: permission.TargetID, TargetName: permission.TargetName,
					ProfileID: permission.ProfileID, ProfileLabel: permission.ProfileLabel,
					ConnectorKind: permission.ConnectorKind, ProfileKind: permission.ProfileKind,
					ActionName: permission.ActionName, ExecutionRule: permission.ExecutionRule, ExpiresAt: permission.ExpiresAt,
				})
			}
			return result, nil
		},
		MetadataEnabled: func(ctx context.Context) (bool, error) {
			settings, err := s.readSecuritySettings(ctx, auth.runtime)
			return settings.ExposeMCPServerMetadata, err
		},
		Metadata: gatewayaccess.NewMCPMetadataResolver(func(kind string) gatewayaccess.MCPMetadataAdapter {
			return s.connectorLiveConsoleTargetAdapterFor(kind)
		}),
	})
	return scope, valid
}

func (s mcpHandlers) mcpConnectorActionScope(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPActionScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayaccess.MCPActionScope{}, false
	}
	call := s.connectorActionApplication().MCPCall(s.connectorActionWorkspace(auth.runtime))
	scope, valid := s.accessOwner.MCPActionScope(auth.runtime, gatewayinfra.MCPActionPorts{
		TokenID: auth.TokenID, RunningHint: s.connectorRunningHint,
		Delivery: s.connectorActionApplication().Delivery,
		Principal: func(tokenID int64) (gatewayaccess.Principal, error) {
			return s.tokenExecutionPrincipal(auth.runtime, tokenID)
		},
		Call: func(ctx context.Context, request gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error) {
			result, err := call(ctx, gatewayactions.Call{
				Source: request.Source, TokenID: request.TokenID, TargetRef: request.TargetRef,
				ActionName: request.ActionName, Input: request.Input, Reason: request.Reason,
				IdempotencyKey: request.IdempotencyKey,
			})
			return gatewayaccess.MCPActionCallResult{Request: result.Request, Result: result.Result, Replayed: result.Replayed}, err
		},
		Observe: func(ctx context.Context, action string, payload any) {
			s.writeObservationAudit(ctx, auth.runtime, "mcp", int64Ptr(auth.TokenID), 0, action, payload)
		},
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, auth.runtime, value)
		},
	})
	return scope, valid
}
