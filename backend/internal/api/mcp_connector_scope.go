package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
)

func (s mcpHandlers) mcpConnectorReadScope(w http.ResponseWriter, r *http.Request) (mcpconnector.Scope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return mcpconnector.Scope{}, false
	}
	return mcpconnector.Scope{
		Database: auth.runtime.database, Registry: auth.runtime.connectorRegistry(), TokenID: auth.TokenID,
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

func (s mcpHandlers) mcpConnectorActionScope(w http.ResponseWriter, r *http.Request) (mcpconnector.ActionScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return mcpconnector.ActionScope{}, false
	}
	return mcpconnector.ActionScope{
		Database: auth.runtime.database, AdapterRegistry: s.connectorAdapterRegistry(), TokenID: auth.TokenID,
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

func mcpConnectorOutputAuthorization(runtime *databaseRuntime) *mcpconnector.OutputAuthorization {
	if runtime == nil {
		return nil
	}
	return &mcpconnector.OutputAuthorization{
		Database: runtime.database, Tokens: runtime.tokens, Leases: runtime.vaultLeases,
		Delivery: actionDeliveryGate{runtime: runtime}, MCPStarted: runtime.isMCPStarted,
		Principal: func(tokenID int64) (executionprincipal.Principal, error) {
			return tokenExecutionPrincipal(runtime, tokenID)
		},
	}
}
