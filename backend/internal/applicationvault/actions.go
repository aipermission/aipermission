package applicationvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/vaultactions"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type ActionDependencies struct {
	Connector     func(*workspaceruntime.Runtime) vaultactions.ConnectorPort
	Mutate        func(context.Context, *workspaceruntime.Runtime, int64, string, func() any, func(*sql.Tx) error) error
	AllowGenerate func(*workspaceruntime.Runtime, int64) bool
}

func (component *Component) ConfigureActions(dependencies ActionDependencies) {
	component.actions = dependencies
}

type actionProjectPort struct{ database *sql.DB }

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

type actionDeliveryPort struct{ runtime *workspaceruntime.Runtime }

func (port actionDeliveryPort) AcquireDelivery(ctx context.Context) (func(), error) {
	if port.runtime == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Security.VaultDelivery.AcquireDelivery(ctx)
}
func (port actionDeliveryPort) AcquireExclusive(ctx context.Context) (func(), error) {
	if port.runtime == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	return port.runtime.Security.VaultDelivery.AcquireExclusive(ctx)
}

type actionMutationPort struct {
	component *Component
	runtime   *workspaceruntime.Runtime
}

func (port actionMutationPort) WithMutation(ctx context.Context, tokenID int64, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime == nil || port.component.actions.Mutate == nil {
		return vaultactions.ErrRuntimeUnavailable
	}
	return port.component.actions.Mutate(ctx, port.runtime, tokenID, action, payload, mutate)
}

func (component *Component) ActionRuntime(runtime *workspaceruntime.Runtime) (*vaultactions.Runtime, error) {
	if component == nil || runtime == nil || runtime.Storage.Database == nil || runtime.Storage.Vault == nil ||
		runtime.Storage.Tokens == nil || runtime.Connectors.ConsoleSessions == nil || runtime.Security.VaultLeases == nil ||
		component.actions.Connector == nil || component.actions.AllowGenerate == nil {
		return nil, vaultactions.ErrRuntimeUnavailable
	}
	items, err := projectvault.NewStore(runtime.Storage.Database, runtime.Storage.Vault, runtime.WorkspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action item store: %w", err)
	}
	owner, err := vaultactions.NewRuntime(vaultactions.Dependencies{
		Database: runtime.Storage.Database, Tokens: runtime.Storage.Tokens,
		Projects: actionProjectPort{database: runtime.Storage.Database}, SessionItems: actionItemPort{store: items},
		ItemMutations: actionItemPort{store: items}, Sessions: runtime.Connectors.ConsoleSessions,
		Leases: runtime.Security.VaultLeases, PersistedLeases: vaultsessions.NewPersistence(runtime.Storage.Database),
		Connector: component.actions.Connector(runtime), Delivery: actionDeliveryPort{runtime: runtime},
		Mutations: actionMutationPort{component: component, runtime: runtime}, WorkspaceID: runtime.WorkspaceUUID,
		RuntimeInstanceID: runtime.RuntimeInstanceID, MCPStarted: runtime.IsMCPStarted,
		AllowGenerate: func(tokenID int64) bool { return component.actions.AllowGenerate(runtime, tokenID) },
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Vault action application: %w", err)
	}
	return owner, nil
}
