// Package catalog owns process-wide connector registries.
package catalog

import (
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type State struct {
	Registry        *connectors.Registry
	AdapterRegistry *connectorapi.Registry
}

func New(registry *connectors.Registry, adapters *connectorapi.Registry) State {
	return State{Registry: registry, AdapterRegistry: adapters}
}
