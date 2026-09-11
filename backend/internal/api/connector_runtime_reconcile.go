package api

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) reconcileConnectorRuntimeSurfaces(ctx context.Context, runtime databaseRuntime) error {
	if runtime == nil || runtime.StoragePort().DatabaseHandle() == nil {
		return nil
	}
	store := connectormgmt.NewStore(runtime.StoragePort().DatabaseHandle())
	targets, err := store.ListTargets(ctx, connectormgmt.ListTargetsFilter{})
	if err != nil {
		return err
	}
	for _, target := range targets {
		profiles, err := store.ListCredentialProfiles(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			if err := s.ensureConnectorRuntimeSurfacesForProfile(ctx, store, target, profile); err != nil {
				return err
			}
		}
	}
	return nil
}
