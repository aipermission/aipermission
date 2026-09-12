package api

import (
	"context"
	"net/http"
	"time"

	gatewayaccesshttp "github.com/aipermission/aipermission/backend/internal/gatewayaccess/httpowner"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s mcpHandlers) mcpVaultScope(w http.ResponseWriter, r *http.Request) (gatewayvault.VaultMCPHTTPScope, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return gatewayvault.VaultMCPHTTPScope{}, false
	}
	scope, valid := s.vaultOwner.VaultMCPScope(auth.runtime, gatewayinfra.VaultMCPPorts{
		TokenID: auth.TokenID,
		Runtime: func(ctx context.Context) (gatewayvault.VaultRequestApplication, error) {
			return s.vaultRequestRuntime(ctx, auth.runtime)
		},
		MetadataRead: func(ctx context.Context, projectID int64) (bool, error) {
			return s.accessOwner.CanReadVaultMetadata(
				ctx, auth.runtime, gatewayaccesshttp.VaultMetadataReaderFactory{},
				auth.TokenID, projectID, time.Now(),
			)
		},
	})
	return scope, valid
}
