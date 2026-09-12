// Package bootstrap owns concrete workspace construction inputs so the
// process composition component does not expose storage implementations.
package bootstrap

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type Adopt struct {
	ID, Path, ConfiguredGatewaySecret string
	Database                          *sql.DB
	Vault                             *vault.Vault
	TokenStore                        *tokens.Store
	Registry                          *connectors.Registry
	AdapterRegistry                   *connectorapi.Registry
	RuntimeInstanceID                 func() (string, error)
}

func (input Adopt) WorkspaceInput() gatewayworkspace.AdoptInput {
	return gatewayworkspace.AdoptInput{
		ID: input.ID, Path: input.Path, ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Database: input.Database, Vault: input.Vault, TokenStore: input.TokenStore,
		Registry: input.Registry, AdapterRegistry: input.AdapterRegistry, RuntimeInstanceID: input.RuntimeInstanceID,
	}
}

type Open struct {
	ID, Path, Password, ConfiguredGatewaySecret string
	Registry                                    *connectors.Registry
	AdapterRegistry                             *connectorapi.Registry
}

func (input Open) WorkspaceInput() gatewayworkspace.OpenInput {
	return gatewayworkspace.OpenInput{
		ID: input.ID, Path: input.Path, Password: input.Password,
		ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Registry:                input.Registry, AdapterRegistry: input.AdapterRegistry,
	}
}
