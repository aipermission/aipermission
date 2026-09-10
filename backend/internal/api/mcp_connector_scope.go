package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

func (s mcpHandlers) mcpConnectorReadScope(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayaccess.MCPScope{}, false
	}
	return gatewayaccess.MCPScope{
		Database: auth.runtime.Storage.Database, Registry: runtimeConnectorRegistry(auth.runtime), TokenID: auth.TokenID,
		Permissions: func(ctx context.Context) ([]gatewayaccess.MCPPermission, error) {
			permissions, err := gatewayaccess.ProjectScopedSupportedConnectorPermissions(
				ctx, auth.runtime.Storage.Database, runtimeConnectorRegistry(auth.runtime), auth.TokenID,
			)
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
			settings, err := readSecuritySettings(ctx, auth.runtime)
			return settings.ExposeMCPServerMetadata, err
		},
		Metadata: func(target connectors.TargetView, profile connectors.CredentialProfileView) map[string]any {
			if adapter := s.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind); adapter != nil {
				return adapter.LiveConsoleTargetMetadata(target, profile)
			}
			return nil
		},
	}, true
}

func (s mcpHandlers) mcpConnectorActionScope(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPActionScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayaccess.MCPActionScope{}, false
	}
	return gatewayaccess.MCPActionScope{
		Database: auth.runtime.Storage.Database, AdapterRegistry: s.connectorAdapterRegistry(), TokenID: auth.TokenID,
		Output: mcpConnectorOutputAuthorization(auth.runtime),
		Call: func(ctx context.Context, call actions.Call) (actions.CallResult, error) {
			return s.callConnectorAction(ctx, auth.runtime, call)
		},
		Observe: func(ctx context.Context, action string, payload any) {
			s.writeObservationAudit(ctx, auth.runtime, "mcp", int64Ptr(auth.TokenID), 0, action, payload)
		},
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, auth.runtime, value)
		},
	}, true
}

func mcpConnectorOutputAuthorization(runtime *databaseRuntime) *gatewayaccess.MCPOutputAuthorization {
	if runtime == nil {
		return nil
	}
	return &gatewayaccess.MCPOutputAuthorization{
		Database: runtime.Storage.Database, Tokens: runtime.Storage.Tokens, Leases: runtime.Security.VaultLeases,
		Delivery: actions.Delivery(runtime), MCPStarted: runtime.IsMCPStarted,
		Principal: func(tokenID int64) (gatewayaccess.Principal, error) {
			return tokenExecutionPrincipal(runtime, tokenID)
		},
	}
}
