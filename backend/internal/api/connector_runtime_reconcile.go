package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func reconcileConnectorRuntimeSurfaces(ctx context.Context, runtime *databaseRuntime) error {
	if runtime == nil || runtime.database == nil {
		return nil
	}
	store := connectortargets.NewStore(runtime.database)
	targets, err := store.ListTargets(ctx, connectortargets.ListTargetsFilter{})
	if err != nil {
		return err
	}
	handlers := connectorTargetHandlers{}
	for _, target := range targets {
		profiles, err := store.ListCredentialProfiles(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			if err := handlers.ensureConnectorRuntimeSurfacesForProfile(ctx, store, target, profile); err != nil {
				return err
			}
		}
	}
	return nil
}
