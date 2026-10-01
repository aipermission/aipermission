package apiadapter

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	resourcecontract "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi/credentialresource"
)

type scopedRuntime struct {
	kinds []string
	store resourcecontract.CredentialResourceStore
}

func TestAdapterCleanupEvidenceExcludesMutableJournalAndSecretAuthority(t *testing.T) {
	adapter := New().(connectorapi.EvidenceCapabilityProvider)
	source := &scopedRuntime{store: journalStore{}}
	values := adapter.EvidenceCapabilities(testEvidenceResources{source})
	if len(values) != 1 || values[rolejournal.CapabilityName] != nil || len(source.kinds) != 1 || source.kinds[0] != rolejournal.ResourceKind {
		t.Fatalf("unexpected evidence scope/authority: %#v %#v", values, source.kinds)
	}
	if _, err := rolejournal.CleanupEvidenceFrom(capabilities(values)); err != nil {
		t.Fatal(err)
	}
	if _, err := rolejournal.FromRuntime(connectors.RuntimeContext{Capabilities: capabilities(values)}); err == nil {
		t.Fatal("cleanup evidence exposed the mutable journal")
	}
	for _, runtime := range []resourcecontract.EvidenceResourceRuntime{nil,
		testEvidenceResources{&scopedRuntime{}},
		testEvidenceResources{&scopedRuntime{store: (*journalStore)(nil)}},
	} {
		if _, err := rolejournal.CleanupEvidenceFrom(capabilities(adapter.EvidenceCapabilities(runtime))); err == nil {
			t.Fatal("missing readonly store accepted")
		}
	}
}

func (runtime *scopedRuntime) CredentialResources(kind string) resourcecontract.CredentialResourceStore {
	runtime.kinds = append(runtime.kinds, kind)
	return runtime.store
}

type testEvidenceResources struct {
	resources resourcecontract.ScopedResourceRuntime
}

func (runtime testEvidenceResources) CredentialResources(kind string) resourcecontract.CredentialResourceReader {
	store := runtime.resources.CredentialResources(kind)
	if resourcecontract.IsNilDependency(store) {
		return nil
	}
	return struct {
		resourcecontract.CredentialResourceReader
	}{store}
}

type capabilities map[string]connectors.RuntimeCapability

func (values capabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	return values[name]
}

type journalStore struct {
	resourcecontract.CredentialResourceStore
}

func TestAdapterExposesOnlyTypedScopedJournal(t *testing.T) {
	adapter := New().(connectorapi.ScopedResourceCapabilityProvider)
	runtime := &scopedRuntime{store: journalStore{}}
	values := adapter.ScopedResourceCapabilities(runtime)
	if len(values) != 1 || len(runtime.kinds) != 1 || runtime.kinds[0] != rolejournal.ResourceKind {
		t.Fatalf("unexpected capability or resource scope: %#v %#v", values, runtime.kinds)
	}
	if _, err := rolejournal.FromRuntime(connectors.RuntimeContext{Capabilities: capabilities(values)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := New().(connectorapi.RuntimeAdapter); ok {
		t.Fatal("journal-only adapter unexpectedly requires action runtime authority")
	}
	for _, runtime := range []resourcecontract.ScopedResourceRuntime{nil, (*scopedRuntime)(nil), &scopedRuntime{}, &scopedRuntime{store: (*journalStore)(nil)}} {
		values := adapter.ScopedResourceCapabilities(runtime)
		if _, err := rolejournal.FromRuntime(connectors.RuntimeContext{Capabilities: capabilities(values)}); err == nil {
			t.Fatal("missing scoped store was accepted")
		}
	}
}
