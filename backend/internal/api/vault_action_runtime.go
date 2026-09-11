package api

import (
	"context"
	"net/http"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultRequestHTTPScope(w http.ResponseWriter) (gatewayvault.VaultApprovalHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayvault.VaultApprovalHTTPScope{}, false
	}
	return gatewayvault.VaultApprovalHTTPScope{
		MCPStarted: runtime.IsMCPStarted,
		Runtime: func(ctx context.Context) (gatewayvault.VaultRequestApplication, error) {
			return s.vaultRequestRuntime(ctx, runtime)
		},
	}, true
}

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime databaseRuntime) (gatewayvault.VaultRequestApplication, error) {
	return s.vaultApplication().RequestRuntime(ctx, s.vaultRuntime(runtime))
}
