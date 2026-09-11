package gatewayinfrastructure

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestComponentOptionsPreserveInjectedCatalogsAndIdentityGenerator(t *testing.T) {
	registry := connectors.NewRegistry()
	adapters := connectorapi.NewRegistry()
	component := NewComponent("test.aipdb", "3212", nil,
		WithConnectorRegistry(registry),
		WithConnectorAdapterRegistry(adapters),
		WithRuntimeInstanceIDGenerator(func() (string, error) { return "runtime-fixture", nil }),
	)
	if component.ConnectorRegistry() != registry || component.ConnectorAdapterRegistry() != adapters {
		t.Fatal("component replaced injected connector catalogs")
	}
	if id, err := component.RuntimeInstanceIDGenerator()(); err != nil || id != "runtime-fixture" {
		t.Fatalf("runtime identity generator = %q, %v", id, err)
	}
}

func TestComponentOptionsFailSafeToDefaultCatalogs(t *testing.T) {
	component := NewComponent("test.aipdb", "3212", nil,
		WithConnectorRegistry(nil),
		WithConnectorAdapterRegistry(nil),
		WithRuntimeInstanceIDGenerator(nil),
	)
	if component.ConnectorRegistry() == nil || component.ConnectorAdapterRegistry() == nil || component.RuntimeInstanceIDGenerator() == nil {
		t.Fatal("component options retained nil process dependencies")
	}
	component.ActivateWorkspace(nil)
	if component.WorkspaceCount() != 0 {
		t.Fatal("nil runtime was activated")
	}
}
