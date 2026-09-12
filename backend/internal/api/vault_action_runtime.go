package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultRequestRuntime(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) (gatewayvault.VaultRequestApplication, error) {
	return s.vaultOwner.VaultRequestApplication(ctx, runtime, s.vaultApplication(), s.vaultRuntimePorts(runtime))
}
