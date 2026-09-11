package gatewayconnectorapi

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func NewRegistry() *connectorapi.Registry { return connectorapi.NewRegistry() }

func NewConnectorRegistry() *connectors.Registry { return connectors.NewRegistry() }
