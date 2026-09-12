package builtin

import (
	"slices"
	"testing"

	dockerconnector "github.com/aipermission/aipermission/backend/internal/connectors/docker"
	kubernetesconnector "github.com/aipermission/aipermission/backend/internal/connectors/kubernetes"
	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
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

func TestNewCatalogExposesOnlyImmutableAdapterSnapshots(t *testing.T) {
	first, err := NewCatalog()
	if err != nil {
		t.Fatalf("new first catalog: %v", err)
	}
	if _, mutable := first.Adapters.(interface {
		Register(string, connectorapi.Adapter) error
	}); mutable {
		t.Fatal("built-in catalog exposes mutable adapter registration")
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
