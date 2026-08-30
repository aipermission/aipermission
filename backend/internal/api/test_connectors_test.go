package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/builtin"
)

type testConnectorCatalog struct {
	connectors *connectors.Registry
	adapters   *connectorapi.Registry
}

func newTestConnectorCatalog(t *testing.T) testConnectorCatalog {
	t.Helper()
	catalog, err := builtin.NewCatalog()
	if err != nil {
		t.Fatalf("new connector catalog: %v", err)
	}
	return testConnectorCatalog{connectors: catalog.Connectors, adapters: catalog.Adapters}
}

func testConnectorRegistry(t *testing.T) *connectors.Registry {
	t.Helper()
	return newTestConnectorCatalog(t).connectors
}
