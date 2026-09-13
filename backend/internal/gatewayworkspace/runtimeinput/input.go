// Package runtimeinput defines inputs accepted by workspace runtime factories.
package runtimeinput

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type Open struct {
	ID, Path, Password, ConfiguredGatewaySecret string
	Registry                                    connectors.Catalog
	AdapterRegistry                             connectorapi.Catalog
}
