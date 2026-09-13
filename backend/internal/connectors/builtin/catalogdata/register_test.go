package catalogdata

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestRegisterInstallsDataConnectors(t *testing.T) {
	registry := connectors.NewRegistry()
	if err := Register(registry); err != nil {
		t.Fatal(err)
	}
	if got := len(registry.List()); got != 6 {
		t.Fatalf("registered data connectors = %d, want 6", got)
	}
	if err := Register(registry); err == nil {
		t.Fatal("duplicate connector registration was accepted")
	}
}
