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

func TestRegisteredCleanupOperationDeclaresGenericLifecyclePolicy(t *testing.T) {
	registry := connectorapi.NewRegistry()
	if err := registry.Register("ssh", New()); err != nil {
		t.Fatal(err)
	}
	value := registry.For("ssh")
	if _, ok := value.(connectorapi.TargetOperationRunner); !ok {
		t.Fatalf("registered adapter lacks operation runner: %T", value)
	}
	policy, ok := value.(connectorapi.TargetOperationLifecyclePolicy)
	if !ok || !policy.RequiresTargetOperationExclusion("key-cleanup-attest") {
		t.Fatal("registered mutation bypasses generic lifecycle exclusion")
	}
	for _, operation := range []string{"key-cleanup-status", "docker-check", "unknown"} {
		if policy.RequiresTargetOperationExclusion(operation) {
			t.Fatalf("registered observation %q claims exclusive mutation admission", operation)
		}
	}
}
