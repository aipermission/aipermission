package api

import (
	"context"
	"net/http"
	"time"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s mcpHandlers) mcpVaultScope(w http.ResponseWriter, r *http.Request) (gatewayvault.VaultMCPHTTPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayvault.VaultMCPHTTPScope{}, false
	}
	return gatewayvault.VaultMCPHTTPScope{
		Database: auth.runtime.Storage.Database, Vault: auth.runtime.Storage.Vault, WorkspaceUUID: auth.runtime.WorkspaceUUID,
		TokenID: auth.TokenID, MCPStarted: auth.runtime.IsMCPStarted,
		Runtime: func(ctx context.Context) (*gatewayvault.VaultRequestRuntime, error) {
			return s.vaultRequestRuntime(ctx, auth.runtime)
		},
		MetadataRead: func(ctx context.Context, projectID int64) (bool, error) {
			capability, err := gatewayaccess.NewCapabilityStore(auth.runtime.Storage.Database).Effective(
				ctx, auth.TokenID, projectID, gatewayaccess.VaultMetadataRead, time.Now(),
			)
			return err == nil && capability.ExecutionRule == gatewayaccess.RuleAlwaysRun, err
		},
	}, true
}
