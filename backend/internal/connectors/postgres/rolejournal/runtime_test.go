package rolejournal

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type testCapabilities map[string]connectors.RuntimeCapability

func (capabilities testCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	return capabilities[name]
}

type wrongCapability struct{}

func (wrongCapability) ConnectorRuntimeCapability() string { return CapabilityName }

func TestFromRuntimeRequiresUsableTypedJournal(t *testing.T) {
	journal := New(newMemoryStore())
	for _, value := range []connectors.RuntimeCapability{nil, (*Journal)(nil), New(nil), New((*memoryStore)(nil)), wrongCapability{}} {
		runtime := connectors.RuntimeContext{Capabilities: testCapabilities{CapabilityName: value}}
		if got, err := FromRuntime(runtime); err == nil || got != nil {
			t.Fatal("unavailable or untyped journal accepted")
		}
	}
	if got, err := FromRuntime(connectors.RuntimeContext{}); err == nil || got != nil {
		t.Fatal("absent resolver accepted")
	}
	got, err := FromRuntime(connectors.RuntimeContext{Capabilities: testCapabilities{CapabilityName: journal}})
	if err != nil || got != journal || got.ConnectorRuntimeCapability() != CapabilityName {
		t.Fatalf("usable capability=%#v error=%v", got, err)
	}
}
