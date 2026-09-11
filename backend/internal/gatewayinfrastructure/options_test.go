package gatewayinfrastructure

import (
	"errors"
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

func TestNilComponentWorkspaceOperationsFailClosed(t *testing.T) {
	var component *Component
	if _, err := component.AdoptWorkspace(t.Context(), AdoptInput{}); !errors.Is(err, ErrInitialization) {
		t.Fatalf("adopt error = %v", err)
	}
	if _, err := component.OpenWorkspace(t.Context(), OpenInput{}); !errors.Is(err, ErrInitialization) {
		t.Fatalf("open error = %v", err)
	}
	if err := component.CloseWorkspace(nil, nil); !errors.Is(err, ErrInitialization) {
		t.Fatalf("close error = %v", err)
	}
	if err := component.MoveDatabase("source", "target"); !errors.Is(err, ErrInitialization) {
		t.Fatalf("move error = %v", err)
	}
	if component.WorkspaceHTTP(WorkspaceHTTPDependencies{}) != nil {
		t.Fatal("nil component exposed workspace handlers")
	}
	if _, err := component.AcquireBackupOperation(t.Context()); !errors.Is(err, ErrInitialization) {
		t.Fatalf("backup operation error = %v", err)
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
