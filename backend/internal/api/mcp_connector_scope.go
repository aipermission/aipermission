package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
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
