package adaptercontainers

import (
	"reflect"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestRegisterInstallsContainerAdapters(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if err := Register(registry); err != nil {
		t.Fatal(err)
	}
	if got, want := registry.Kinds(), []string{"docker", "kubernetes"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter kinds = %#v, want %#v", got, want)
	}
	if err := Register(registry); err == nil {
		t.Fatal("duplicate adapter registration was accepted")
	}
}
