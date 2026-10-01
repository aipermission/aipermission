package rolejournal

import (
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestCleanupEvidenceCapabilityRequiresExactReadonlyTypeAndStore(t *testing.T) {
	for _, value := range []connectors.RuntimeCapability{nil, (*CleanupEvidence)(nil), NewCleanupEvidence(nil), NewCleanupEvidence((*memoryStore)(nil)), New(newMemoryStore()), wrongCapability{}} {
		if got, err := CleanupEvidenceFrom(testCapabilities{CleanupEvidenceCapabilityName: value}); err == nil || got != nil {
			t.Fatal("missing or mutable cleanup evidence accepted")
		}
	}
	for _, values := range []connectors.RuntimeCapabilityResolver{nil, testCapabilities(nil)} {
		if got, err := CleanupEvidenceFrom(values); err == nil || got != nil {
			t.Fatal("nil or typed-nil evidence resolver accepted")
		}
	}
	evidence := NewCleanupEvidence(newMemoryStore())
	if got, err := CleanupEvidenceFrom(testCapabilities{CleanupEvidenceCapabilityName: evidence}); err != nil || got != evidence {
		t.Fatalf("readonly evidence unavailable: %#v %v", got, err)
	}
	typeOf := reflect.TypeOf(evidence)
	if typeOf.NumMethod() != 2 || typeOf.Method(0).Name != "ConfirmedCleanup" || typeOf.Method(1).Name != "ConnectorRuntimeCapability" {
		t.Fatalf("cleanup evidence acquired mutation authority: %v", typeOf)
	}
}
