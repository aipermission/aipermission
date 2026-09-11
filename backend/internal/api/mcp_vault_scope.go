package api

import (
	"context"
	"net/http"
	"time"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s mcpHandlers) mcpVaultScope(w http.ResponseWriter, r *http.Request) (gatewayvault.VaultMCPHTTPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayvault.VaultMCPHTTPScope{}, false
	}
	return gatewayvault.VaultMCPHTTPScope{
		Database: auth.runtime.Storage.DatabaseHandle(), Vault: auth.runtime.Storage.SecretVault(), WorkspaceUUID: auth.runtime.Identity.WorkspaceID,
		TokenID: auth.TokenID, MCPStarted: func() bool { return auth.runtime.Security.RuntimeControlState().MCPStarted() },
		Runtime: func(ctx context.Context) (gatewayvault.VaultRequestApplication, error) {
			return s.vaultRequestRuntime(ctx, auth.runtime)
		},
		MetadataRead: func(ctx context.Context, projectID int64) (bool, error) {
			return s.access.CanReadVaultMetadata(
				ctx, auth.runtime.Storage.DatabaseHandle(), auth.TokenID, projectID, time.Now(),
			)
		},
	}, true
}
