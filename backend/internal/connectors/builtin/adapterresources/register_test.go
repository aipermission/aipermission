package adapterresources

import (
	"reflect"
	"testing"

	s3connector "github.com/aipermission/aipermission/backend/internal/connectors/s3"
	s3apiadapter "github.com/aipermission/aipermission/backend/internal/connectors/s3/apiadapter"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestRegisterInstallsResourceAdapters(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if err := Register(registry); err != nil {
		t.Fatal(err)
	}
	if got, want := registry.Kinds(), []string{"postgres", "s3", "ssh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("adapter kinds = %#v, want %#v", got, want)
	}
	if err := Register(registry); err == nil {
		t.Fatal("duplicate adapter registration was accepted")
	}
}

func TestRegisterRejectsLaterDuplicateResourceAdapter(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if err := registry.Register(s3connector.Kind, s3apiadapter.New()); err != nil {
		t.Fatal(err)
	}
	if err := Register(registry); err == nil {
		t.Fatal("later duplicate resource adapter was accepted")
	}
}
