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
		Database: auth.runtime.StoragePort().DatabaseHandle(), Vault: auth.runtime.StoragePort().SecretVault(), WorkspaceUUID: auth.runtime.WorkspaceIdentifier(),
		TokenID: auth.TokenID, MCPStarted: auth.runtime.IsMCPStarted,
		Runtime: func(ctx context.Context) (gatewayvault.VaultRequestApplication, error) {
			return s.vaultRequestRuntime(ctx, auth.runtime)
		},
		MetadataRead: func(ctx context.Context, projectID int64) (bool, error) {
			capability, err := s.access.NewCapabilityStore(auth.runtime.StoragePort().DatabaseHandle()).Effective(
				ctx, auth.TokenID, projectID, gatewayaccess.VaultMetadataRead, time.Now(),
			)
			return err == nil && capability.ExecutionRule == gatewayaccess.RuleAlwaysRun, err
		},
	}, true
}
