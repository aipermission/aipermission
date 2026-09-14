package api

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestServerOptionsDetachConnectorCatalogsFromBootstrapBuilders(t *testing.T) {
	catalog := newTestConnectorCatalog(t)
	resolved := resolveServerOptions([]ServerOption{
		WithConnectorRegistry(catalog.connectors),
		WithConnectorAdapterRegistry(catalog.adapters),
	})
	if resolved.err != nil {
		t.Fatal(resolved.err)
	}
	if err := catalog.connectors.Register(localActionTestConnector{}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.adapters.Register(localActionTestConnectorKind, struct{}{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := resolved.registry.Get(localActionTestConnectorKind); ok {
		t.Fatal("post-composition connector registration changed the server catalog")
	}
	if adapter := resolved.adapterRegistry.For(localActionTestConnectorKind); adapter != nil {
		t.Fatalf("post-composition adapter registration changed the server catalog: %T", adapter)
	}
	if _, mutable := resolved.registry.(interface {
		Register(connectors.Connector) error
	}); mutable {
		t.Fatal("server connector catalog exposes a mutable facade")
	}
	if _, mutable := resolved.adapterRegistry.(interface {
		Register(string, connectorapi.Adapter) error
	}); mutable {
		t.Fatal("server adapter catalog exposes a mutable facade")
	}
}
