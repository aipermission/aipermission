package builtin

import (
	"slices"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
)

type isolatedTestAdapter struct{}

func TestNewCatalogRegistersRuntimeAdaptersExplicitly(t *testing.T) {
	catalog, err := NewCatalog()
	if err != nil {
		t.Fatalf("new catalog: %v", err)
	}
	expectedAdapterKinds := []string{
		dockerconnector.Kind,
		kubernetesconnector.Kind,
		s3connector.Kind,
		sshconnector.Kind,
	}
	slices.Sort(expectedAdapterKinds)
	if got := catalog.Adapters.Kinds(); !slices.Equal(got, expectedAdapterKinds) {
		t.Fatalf("adapter kinds = %v, want %v", got, expectedAdapterKinds)
	}
	connectorKinds := map[string]bool{}
	for _, info := range catalog.Connectors.List() {
		connectorKinds[info.Kind] = true
	}
	for _, kind := range expectedAdapterKinds {
		if adapter := catalog.Adapters.For(kind); adapter == nil {
			t.Errorf("adapter %q is not registered", kind)
		}
		if !connectorKinds[kind] {
			t.Errorf("adapter %q has no built-in connector", kind)
		}
	}
}

func TestNewCatalogDoesNotShareAdapterState(t *testing.T) {
	first, err := NewCatalog()
	if err != nil {
		t.Fatalf("new first catalog: %v", err)
	}
	second, err := NewCatalog()
	if err != nil {
		t.Fatalf("new second catalog: %v", err)
	}
	if err := first.Adapters.Register("test-only", isolatedTestAdapter{}); err != nil {
		t.Fatalf("register isolated adapter: %v", err)
	}
	if adapter := second.Adapters.For("test-only"); adapter != nil {
		t.Fatalf("second catalog inherited adapter state: %T", adapter)
	}
}

func TestAdapterRegistryRejectsDuplicateBuiltInRegistration(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if err := RegisterAdapters(registry); err != nil {
		t.Fatalf("register adapters: %v", err)
	}
	if err := RegisterAdapters(registry); err == nil {
		t.Fatal("duplicate built-in adapter registration succeeded")
	}
}
