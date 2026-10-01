package connectorruntime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type evidenceTestResources struct {
	store connectorapi.CredentialResourceStore
	kind  string
}

func (resources *evidenceTestResources) CredentialResources(kind string) connectorapi.CredentialResourceStore {
	resources.kind = kind
	return resources.store
}

type evidenceTestStore struct {
	connectorapi.CredentialResourceStore
	read func(context.Context, int64) (connectorapi.CredentialResource, error)
}

func (store *evidenceTestStore) Get(ctx context.Context, id int64) (connectorapi.CredentialResource, error) {
	return store.read(ctx, id)
}

func TestEvidenceResourcesExposeOnlyGetAndPreserveScopeAndErrors(t *testing.T) {
	readFailure := errors.New("fixture read failed")
	row := connectorapi.CredentialResource{ID: 8, ResourceType: "domain.v1", PublicData: `{"status":"confirmed"}`}
	resources := &evidenceTestResources{store: &evidenceTestStore{read: func(ctx context.Context, id int64) (connectorapi.CredentialResource, error) {
		if ctx != t.Context() {
			t.Fatal("reader replaced caller context")
		}
		if id == row.ID {
			return row, nil
		}
		return connectorapi.CredentialResource{}, readFailure
	}}}
	runtime := EvidenceResources(resources)
	reader := runtime.CredentialResources("domain_journal")
	if resources.kind != "domain_journal" {
		t.Fatal("evidence runtime dropped connector resource-class scope")
	}
	for _, scenario := range []struct {
		value any
		name  string
	}{{runtime, "CredentialResources"}, {reader, "Get"}} {
		typeOf := reflect.TypeOf(scenario.value)
		if typeOf.NumMethod() != 1 || typeOf.Method(0).Name != scenario.name {
			t.Fatalf("evidence authority grew: %v", typeOf)
		}
	}
	if _, mutable := reader.(connectorapi.CredentialResourceStore); mutable {
		t.Fatal("reader recovered write/secret authority by type assertion")
	}
	if _, mutable := any(runtime).(connectorapi.ScopedResourceRuntime); mutable {
		t.Fatal("evidence runtime recovered mutable resource authority")
	}
	if got, err := reader.Get(t.Context(), row.ID); err != nil || got != row {
		t.Fatalf("public evidence changed: %#v %v", got, err)
	}
	if _, err := reader.Get(t.Context(), row.ID+1); !errors.Is(err, readFailure) {
		t.Fatalf("resource error lost: %v", err)
	}
}

func TestEvidenceResourcesRejectUnavailableBackingStores(t *testing.T) {
	for _, resources := range []connectorapi.ScopedResourceRuntime{nil, (*evidenceTestResources)(nil)} {
		if got := EvidenceResources(resources); got != nil {
			t.Fatal("nil or typed-nil resource runtime accepted")
		}
	}
	for _, store := range []connectorapi.CredentialResourceStore{nil, (*evidenceTestStore)(nil)} {
		if got := EvidenceResources(&evidenceTestResources{store: store}).CredentialResources("domain_journal"); got != nil {
			t.Fatal("nil or typed-nil backing store accepted")
		}
	}
}
