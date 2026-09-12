package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	"net/http"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultRequestHTTPScope(w http.ResponseWriter) (gatewayvault.VaultApprovalHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayvault.VaultApprovalHTTPScope{}, false
	}
	scope, valid := s.vaultOwner.VaultApprovalScope(runtime, func(ctx context.Context) (gatewayvault.VaultRequestApplication, error) {
		return s.vaultRequestRuntime(ctx, runtime)
	})
	return scope, valid
}

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) (gatewayvault.VaultRequestApplication, error) {
	return s.vaultApplication().RequestRuntime(ctx, s.vaultRuntime(runtime))
}
