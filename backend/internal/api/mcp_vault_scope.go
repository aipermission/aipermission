package api

import (
	"context"
	"net/http"
	"time"

	gatewayaccesshttp "github.com/aipermission/aipermission/backend/internal/gatewayaccess/httpowner"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s mcpHandlers) mcpVaultPorts(w http.ResponseWriter, r *http.Request) (*gatewayinfra.WorkspaceHandle, gatewayinfra.VaultMCPHTTPPorts, bool) {
	auth, ok := s.authenticateMCP(w, r)
	if !ok {
		return nil, gatewayinfra.VaultMCPHTTPPorts{}, false
	}
	ports := gatewayinfra.VaultMCPHTTPPorts{
		TokenID: auth.TokenID,
		MetadataRead: func(ctx context.Context, projectID int64) (bool, error) {
			return s.accessOwner.CanReadVaultMetadata(
				ctx, auth.runtime, gatewayaccesshttp.VaultMetadataReaderFactory{},
				auth.TokenID, projectID, time.Now(),
			)
		},
	}
	return auth.runtime, ports, true
}
