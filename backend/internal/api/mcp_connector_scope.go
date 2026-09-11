package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s mcpHandlers) mcpConnectorReadScope(w http.ResponseWriter, r *http.Request) (gatewayaccess.MCPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayaccess.MCPScope{}, false
	}
	return gatewayaccess.MCPScope{
		Database: auth.runtime.StoragePort().DatabaseHandle(), Registry: runtimeConnectorRegistry(auth.runtime), TokenID: auth.TokenID,
		Permissions: func(ctx context.Context) ([]gatewayaccess.MCPPermission, error) {
			permissions, err := connectormgmt.ProjectScopedSupportedConnectorPermissions(
				ctx, auth.runtime.StoragePort().DatabaseHandle(), runtimeConnectorRegistry(auth.runtime), auth.TokenID,
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
		Database: auth.runtime.StoragePort().DatabaseHandle(), AdapterRegistry: s.connectorAdapterRegistry(), TokenID: auth.TokenID,
		Output: s.mcpConnectorOutputAuthorization(auth.runtime),
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

func (s *Server) mcpConnectorOutputAuthorization(runtime databaseRuntime) *gatewayaccess.MCPOutputAuthorization {
	if runtime == nil {
		return nil
	}
	return &gatewayaccess.MCPOutputAuthorization{
		Database: runtime.StoragePort().DatabaseHandle(), Tokens: runtime.StoragePort().TokenStore(), Leases: runtime.SecurityPort().VaultLeaseStore(),
		Delivery: s.connectorActionApplication().Delivery(runtime.SecurityPort().VaultDeliveryCoordinator().AcquireDelivery), MCPStarted: runtime.IsMCPStarted,
		Principal: func(tokenID int64) (gatewayaccess.Principal, error) {
			return s.tokenExecutionPrincipal(runtime, tokenID)
		},
	}
}
