package adapterresources

import (
	"reflect"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestRegisterInstallsResourceAdapters(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if err := Register(registry); err != nil {
		t.Fatal(err)
	}
	if got, want := registry.Kinds(), []string{"s3", "ssh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter kinds = %#v, want %#v", got, want)
	}
	if err := Register(registry); err == nil {
		t.Fatal("duplicate adapter registration was accepted")
	}
}
