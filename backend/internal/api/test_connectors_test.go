package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type testConnectorCatalog struct {
	connectors *connectors.Registry
	adapters   *connectorapi.Registry
}

func newTestConnectorCatalog(t *testing.T) testConnectorCatalog {
	t.Helper()
	connectorRegistry := connectors.NewRegistry()
	if err := builtin.RegisterAll(connectorRegistry); err != nil {
		t.Fatalf("register built-in connectors: %v", err)
	}
	adapterRegistry := connectorapi.NewRegistry()
	if err := builtin.RegisterAdapters(adapterRegistry); err != nil {
		t.Fatalf("register built-in connector adapters: %v", err)
	}
	return testConnectorCatalog{connectors: connectorRegistry, adapters: adapterRegistry}
}

type testCatalogOption func(testing.TB, testConnectorCatalog)

func withTestConnector(connector connectors.Connector) testCatalogOption {
	return func(t testing.TB, catalog testConnectorCatalog) {
		t.Helper()
		if err := catalog.connectors.Register(connector); err != nil {
			t.Fatalf("register test connector: %v", err)
		}
	}
}

func withTestConnectorAdapter(kind string, adapter connectorapi.Adapter) testCatalogOption {
	return func(t testing.TB, catalog testConnectorCatalog) {
		t.Helper()
		if err := catalog.adapters.Register(kind, adapter); err != nil {
			t.Fatalf("register test connector adapter: %v", err)
		}
	}
}

func testConnectorRegistry(t *testing.T) *connectors.Registry {
	t.Helper()
	return newTestConnectorCatalog(t).connectors
}
