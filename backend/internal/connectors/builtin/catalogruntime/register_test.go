package catalogruntime

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestRegisterInstallsRuntimeConnectors(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := Register(registry); err != nil {
		t.Fatal(err)
	}
	if got := len(registry.List()); got != 4 {
		t.Fatalf("registered runtime connectors = %d, want 4", got)
	}
	if err := Register(registry); err == nil {
		t.Fatal("duplicate connector registration was accepted")
	}
}
