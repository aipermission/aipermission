// Package runtimeinput defines inputs accepted by workspace runtime factories.
package runtimeinput

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
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

type Open struct {
	ID, Path, Password, ConfiguredGatewaySecret string
	Registry                                    *connectors.Registry
	AdapterRegistry                             *connectorapi.Registry
}
