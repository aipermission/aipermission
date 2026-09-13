package apiadapter

import (
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestNewExposesSSHRuntimeContracts(t *testing.T) {
	value := New()
	if value == nil {
		t.Fatal("SSH adapter is nil")
	}
	if _, ok := value.(connectorapi.LiveConsoleAdapter); !ok {
		t.Fatalf("SSH adapter lacks live console contract: %T", value)
	}
	if _, ok := value.(connectorapi.FileTransferAdapter); !ok {
		t.Fatalf("SSH adapter lacks file transfer contract: %T", value)
	}
}
