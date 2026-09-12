// Package builtin registers connector implementations shipped with
// AIPermission.
package builtin

import (
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin/adaptercontainers"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin/adapterresources"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin/catalogdata"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin/catalogruntime"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type Catalog struct {
	Connectors connectors.Catalog
	Adapters   connectorapi.Catalog
}

// RegisterAll adds all built-in connectors to the provided registry.
func RegisterAll(registry *connectors.Registry) error {
	for _, register := range []func(*connectors.Registry) error{
		catalogdata.Register,
		catalogruntime.Register,
	} {
		if err := register(registry); err != nil {
			return err
		}
	}
	return nil
}

// NewRegistry returns a registry populated with all built-in connectors.
func NewRegistry() (*connectors.Registry, error) {
	registry := connectors.NewRegistry()
	if err := RegisterAll(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func RegisterAdapters(registry *connectorapi.Registry) error {
	for _, register := range []func(*connectorapi.Registry) error{
		adaptercontainers.Register,
		adapterresources.Register,
	} {
		if err := register(registry); err != nil {
			return err
		}
	}
	return nil
}

func NewAdapterRegistry() (*connectorapi.Registry, error) {
	registry := connectorapi.NewRegistry()
	if err := RegisterAdapters(registry); err != nil {
		return nil, err
	}
	return registry, nil
}

func NewCatalog() (Catalog, error) {
	connectorRegistry, err := NewRegistry()
	if err != nil {
		return Catalog{}, err
	}
	adapterRegistry, err := NewAdapterRegistry()
	if err != nil {
		return Catalog{}, err
	}
	return Catalog{Connectors: connectorRegistry.Snapshot(), Adapters: adapterRegistry.Snapshot()}, nil
}
