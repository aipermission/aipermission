package connectors

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type State struct {
	registry        *connectorcatalog.Registry
	adapterRegistry *connectorapi.Registry
	resources       connectorruntime.ResourceScopes
	consoleSessions *console.Manager
	database        *sql.DB
	vault           *vault.Vault
	workspaceID     string
}

func New(
	registry *connectorcatalog.Registry,
	adapterRegistry *connectorapi.Registry,
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

func (s *State) ConnectorRegistry() *connectorcatalog.Registry {
	if s == nil {
		return nil
	}
	return s.registry
}

func (s *State) ConnectorAdapterRegistry() *connectorapi.Registry {
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

func (s *State) ConnectorScope(kind string, accessor connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	if s == nil {
		return connectorruntime.NewScope(kind, connectorruntime.Dependencies{})
	}
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{
		Database: s.database, Vault: s.vault, WorkspaceID: s.workspaceID,
		Resources: s.resources, ConsoleSessions: s.consoleSessions, SecretAccessor: accessor,
	})
}

var _ Port = (*State)(nil)
