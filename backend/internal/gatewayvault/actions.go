package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type actionProjectPort struct{ database *sql.DB }

type VaultActionApplication interface {
	BuildEnvironmentPlan(context.Context, int64, []projectvault.SessionSelection) (vaultactions.EnvironmentPlan, error)
	Prepare(context.Context, int64, string, string, map[string]any) (vaultrequests.PreparedAction, error)
	AuthorizeOutput(context.Context, vaultrequests.Request) bool
	ValidateAuthorization(context.Context, vaultrequests.Request, vaultrequests.ApprovalContext) error
	Execute(context.Context, vaultrequests.Request) (any, error)
	Compensate(context.Context, vaultrequests.Request, any) error
	IsStale(error) bool
}

func (port actionProjectPort) ResolveRef(ctx context.Context, ref string) (vaultactions.Project, bool, error) {
	project, err := projectstore.NewStore(port.database).ResolveRef(ctx, ref)
	if errors.Is(err, projectstore.ErrNotFound) {
		return vaultactions.Project{}, false, nil
	}
	if err != nil {
		return vaultactions.Project{}, false, err
	}
	return vaultactions.Project{ID: project.ID, Name: project.Name, Slug: project.Slug}, true, nil
}

func (port actionProjectPort) TokenCanAccess(ctx context.Context, tokenID, projectID int64) (bool, error) {
	return projectstore.NewStore(port.database).TokenCanAccessProject(ctx, tokenID, projectID)
}

type actionItemPort struct{ store *projectvault.Store }

func (port actionItemPort) SnapshotSession(ctx context.Context, items []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return port.store.SnapshotSession(ctx, items)
}
func (port actionItemPort) ResolveSession(ctx context.Context, items []projectvault.SessionSelection) (projectvault.SessionResolution, error) {
	return port.store.ResolveSession(ctx, items)
}
func (port actionItemPort) RevalidateSession(ctx context.Context, items []projectvault.SessionItem) error {
	return port.store.RevalidateSession(ctx, items)
}
func (port actionItemPort) RecordSessionItems(ctx context.Context, id int64, items []projectvault.SessionItem) error {
	return port.store.RecordSessionItems(ctx, id, items)
}
func (port actionItemPort) MarkSessionItemsUsed(ctx context.Context, items []projectvault.SessionItem) error {
	return port.store.MarkSessionItemsUsed(ctx, items)
}
func (port actionItemPort) Create(ctx context.Context, tx *sql.Tx, input projectvault.CreateInput) (projectvault.Item, error) {
	return port.store.WithTx(tx).Create(ctx, input)
}
func (port actionItemPort) Delete(ctx context.Context, id, valueVersion, metadataRevision int64) error {
	return port.store.Delete(ctx, id, valueVersion, metadataRevision)
}

type actionDeliveryPort struct{ runtime Runtime }

func (port actionDeliveryPort) AcquireDelivery(ctx context.Context) (func(), error) {
	if port.runtime.Session.AcquireDelivery == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Session.AcquireDelivery(ctx)
}
func (port actionDeliveryPort) AcquireExclusive(ctx context.Context) (func(), error) {
	if port.runtime.Session.AcquireExclusive == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Session.AcquireExclusive(ctx)
}

type actionMutationPort struct {
	component *Component
	runtime   Runtime
}

func (port actionMutationPort) WithMutation(ctx context.Context, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime.Action.Mutate == nil {
		return vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Action.Mutate(ctx, tokenID, action, payload, mutate)
}

func (component *Component) ActionRuntime(runtime Runtime) (VaultActionApplication, error) {
	if component == nil || runtime.Storage.Database == nil || runtime.Storage.SecretVault == nil || runtime.Storage.Tokens == nil || runtime.Session.Sessions == nil ||
		runtime.Session.Leases == nil || runtime.Action.Connector == nil || runtime.Action.AllowGenerate == nil || runtime.Session.MCPStarted == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	items, err := projectvault.NewStore(runtime.Storage.Database, runtime.Storage.SecretVault, runtime.Storage.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action item store: %w", err)
	}
	owner, err := vaultactions.NewRuntime(vaultactions.Dependencies{
		Database: runtime.Storage.Database, Tokens: runtime.Storage.Tokens,
		Projects: actionProjectPort{database: runtime.Storage.Database}, SessionItems: actionItemPort{store: items},
		ItemMutations: actionItemPort{store: items}, Sessions: runtime.Session.Sessions,
		Leases: runtime.Session.Leases, PersistedLeases: vaultsessions.NewPersistence(runtime.Storage.Database),
		Connector: runtime.Action.Connector, Delivery: actionDeliveryPort{runtime: runtime},
		Mutations: actionMutationPort{component: component, runtime: runtime}, WorkspaceID: runtime.Storage.WorkspaceID,
		RuntimeInstanceID: runtime.Session.RuntimeInstanceID, MCPStarted: runtime.Session.MCPStarted,
		AllowGenerate: runtime.Action.AllowGenerate,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action application: %w", err)
	}
	return owner, nil
}
