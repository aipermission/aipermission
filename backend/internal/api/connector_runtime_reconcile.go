package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) reconcileConnectorRuntimeSurfaces(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle) error {
	if runtime == nil {
		return nil
	}
	return s.connectorCatalog(runtime).ReconcileRuntimeSurfaces(ctx)
}
