package gatewayconnectormanagement

import (
	"context"
	"database/sql"
	"time"

	"github.com/aipermission/aipermission/backend/internal/accesscontrol"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

type Catalog struct {
	component *Component
	database  *sql.DB
	registry  *connectors.Registry
}

func (component *Component) Catalog(database *sql.DB, registry *connectors.Registry) Catalog {
	return Catalog{component: component, database: database, registry: registry}
}

func (catalog Catalog) store() *connectortargets.Store {
	return connectortargets.NewStore(catalog.database)
}

func (catalog Catalog) Target(ctx context.Context, id int64) (connectortargets.Target, error) {
	return catalog.store().GetTarget(ctx, id)
}

func (catalog Catalog) ResolveActionTarget(ctx context.Context, targetRef string) (connectors.TargetView, connectors.CredentialProfileView, error) {
	return catalog.store().ResolveConnectorActionTarget(ctx, targetRef)
}

func (catalog Catalog) TargetProfileByRuntimeID(ctx context.Context, runtimeID int64) (connectors.TargetView, connectors.CredentialProfileView, connectortargets.RuntimeSurface, error) {
	return catalog.store().TargetProfileByRuntimeID(ctx, runtimeID)
}

func (catalog Catalog) RuntimeSurface(ctx context.Context, runtimeID int64) (connectortargets.RuntimeSurface, error) {
	return catalog.store().GetRuntimeSurface(ctx, runtimeID)
}

func (catalog Catalog) ActionPermission(ctx context.Context, tokenID, targetID, profileID int64, action string, now time.Time) (connectortargets.ActionPermission, error) {
	return catalog.store().GetActionPermission(ctx, tokenID, targetID, profileID, action, now)
}

func (catalog Catalog) ProjectScopedSupportedConnectorPermissions(ctx context.Context, tokenID int64) ([]connectortargets.ActionPermission, error) {
	return accesscontrol.ProjectScopedSupportedConnectorPermissions(ctx, catalog.database, catalog.registry, tokenID)
}

func (catalog Catalog) ValidateTargetTransport(ctx context.Context, projectID int64, config map[string]any) error {
	return ValidateTransport(ctx, catalog.store(), projectID, config, catalog.component.dependencies.Capabilities.HasTCPTransport)
}

func (catalog Catalog) ReconcileRuntimeSurfaces(ctx context.Context) error {
	store := catalog.store()
	targets, err := store.ListTargets(ctx, connectortargets.ListTargetsFilter{})
	if err != nil {
		return err
	}
	for _, target := range targets {
		profiles, err := store.ListCredentialProfiles(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, profile := range profiles {
			if err := catalog.component.ensureRuntimeSurfaces(ctx, store, target, profile); err != nil {
				return err
			}
		}
	}
	return nil
}

func (catalog Catalog) EnsureRuntimeSurfacesInTx(ctx context.Context, tx *sql.Tx, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
	if tx == nil {
		return nil
	}
	return catalog.component.ensureRuntimeSurfaces(ctx, connectortargets.NewTxStore(tx), target, profile)
}
