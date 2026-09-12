// Package runtimeinput defines inputs accepted by workspace runtime factories.
package runtimeinput

import (
	"database/sql"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type Adopt struct {
	ID, Path, ConfiguredGatewaySecret string
	Database                          *sql.DB
	Vault                             *vault.Vault
	TokenStore                        *tokens.Store
	Registry                          connectors.Catalog
	AdapterRegistry                   connectorapi.Catalog
	RuntimeInstanceID                 func() (string, error)
}

type Open struct {
	ID, Path, Password, ConfiguredGatewaySecret string
	Registry                                    connectors.Catalog
	AdapterRegistry                             connectorapi.Catalog
}
