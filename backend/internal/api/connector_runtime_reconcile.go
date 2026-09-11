package api

import (
	"context"
)

func (s *Server) reconcileConnectorRuntimeSurfaces(ctx context.Context, runtime databaseRuntime) error {
	if runtime == nil || runtime.StoragePort().DatabaseHandle() == nil {
		return nil
	}
	return s.connectorCatalog(runtime).ReconcileRuntimeSurfaces(ctx)
}
