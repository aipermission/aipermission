package connectors

import (
	"context"
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type State struct {
	registry        connectorcatalog.Catalog
	adapterRegistry connectorapi.Catalog
	resources       connectorruntime.ResourceScopes
	consoleSessions *console.Manager
	database        *sql.DB
	vault           *vault.Vault
	workspaceID     string
}

func New(
	registry connectorcatalog.Catalog,
	adapterRegistry connectorapi.Catalog,
	database *sql.DB,
	secretVault *vault.Vault,
	workspaceUUID string,
) State {
	return State{
		registry: registry, adapterRegistry: adapterRegistry,
		resources: connectorruntime.NewResourceScopes(database, secretVault, workspaceUUID),
		database:  database, vault: secretVault, workspaceID: workspaceUUID,
	}
}

func (s *State) ConnectorRegistry() connectorcatalog.Catalog {
	if s == nil {
		return nil
	}
	return s.registry
}

func (s *State) ConnectorAdapterRegistry() connectorapi.Catalog {
	if s == nil {
		return nil
	}
	return s.adapterRegistry
}

func (s *State) ResourceScopes() connectorruntime.ResourceScopes {
	if s == nil {
		return nil
	}
	return s.resources
}

func (s *State) ConsoleSessionManager() *console.Manager {
	if s == nil {
		return nil
	}
	return s.consoleSessions
}

func (s *State) ConfigureConsoleSessions(openRuntime console.RuntimeOpener, redact func(string) string) {
	if s != nil {
		s.consoleSessions = console.NewManager(s.database, openRuntime, redact)
	}
}

func (s *State) ConfigureVaultSessionAuthorizer(leases *vaultsessions.Store, guard func(context.Context, func() error, func() error) error) {
	if s == nil || s.consoleSessions == nil || leases == nil || guard == nil {
		return
	}
	s.consoleSessions.SetAuthorizer(func(ctx context.Context, principal executionprincipal.Principal, session console.SessionAuthorization, operation console.SessionOperation, run func() error) error {
		return guard(ctx, func() error { return leases.Authorize(ctx, principal, session, operation) }, run)
	})
}

func (s *State) ConfigureSessionClosedHook(closed func(context.Context, int64, int64, int64) error) {
	if s == nil || s.consoleSessions == nil || closed == nil {
		return
	}
	s.consoleSessions.SetSessionClosedHook(func(ctx context.Context, session console.SessionHandle) error {
		return closed(ctx, session.ID, session.RuntimeID, session.Generation)
	})
}

func (s *State) ConnectorScope(kind string, accessor connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	if s == nil {
		return connectorruntime.NewScope(kind, connectorruntime.Dependencies{})
	}
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{
		Database: s.database, Vault: s.vault, WorkspaceID: s.workspaceID,
		Resources: s.resources, ConsoleSessions: s.consoleSessions, SecretAccessor: accessor,
	})
}
