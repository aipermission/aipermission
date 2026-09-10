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
