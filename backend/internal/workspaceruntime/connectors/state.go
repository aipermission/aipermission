package connectors

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	connectorcatalog "github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type State struct {
	Registry        *connectorcatalog.Registry
	AdapterRegistry *connectorapi.Registry
	Resources       connectorruntime.ResourceScopes
	ConsoleSessions *console.Manager
	database        *sql.DB
	vault           *vault.Vault
	workspaceID     string
}

type Port interface {
	ConnectorRegistry() *connectorcatalog.Registry
	ConnectorAdapterRegistry() *connectorapi.Registry
	ResourceScopes() connectorruntime.ResourceScopes
	ConsoleSessionManager() *console.Manager
	SetConsoleSessionManager(*console.Manager)
	ConnectorScope(string, connectorruntime.SecretAccessorFactory) *connectorruntime.Scope
}

func New(
	registry *connectorcatalog.Registry,
	adapterRegistry *connectorapi.Registry,
	database *sql.DB,
	secretVault *vault.Vault,
	workspaceUUID string,
) State {
	return State{
		Registry:        registry,
		AdapterRegistry: adapterRegistry,
		Resources:       connectorruntime.NewResourceScopes(database, secretVault, workspaceUUID),
		database:        database,
		vault:           secretVault,
		workspaceID:     workspaceUUID,
	}
}

func (s *State) ConnectorRegistry() *connectorcatalog.Registry {
	if s != nil && s.Registry != nil {
		return s.Registry
	}
	return connectorcatalog.NewRegistry()
}

func (s *State) ConnectorAdapterRegistry() *connectorapi.Registry {
	if s != nil && s.AdapterRegistry != nil {
		return s.AdapterRegistry
	}
	return connectorapi.NewRegistry()
}

func (s *State) ResourceScopes() connectorruntime.ResourceScopes {
	if s == nil {
		return nil
	}
	return s.Resources
}

func (s *State) ConsoleSessionManager() *console.Manager {
	if s == nil {
		return nil
	}
	return s.ConsoleSessions
}

func (s *State) SetConsoleSessionManager(manager *console.Manager) {
	if s != nil {
		s.ConsoleSessions = manager
	}
}

func (s *State) ConnectorScope(kind string, accessor connectorruntime.SecretAccessorFactory) *connectorruntime.Scope {
	if s == nil {
		return connectorruntime.NewScope(kind, connectorruntime.Dependencies{})
	}
	return connectorruntime.NewScope(kind, connectorruntime.Dependencies{
		Database: s.database, Vault: s.vault, WorkspaceID: s.workspaceID,
		Resources: s.Resources, ConsoleSessions: s.ConsoleSessions, SecretAccessor: accessor,
	})
}
