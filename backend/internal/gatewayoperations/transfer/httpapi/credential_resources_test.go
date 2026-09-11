package filetransferhttp

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type mutableCredentialResourceStore struct {
	record      connectorapi.CredentialResource
	secret      map[string]any
	afterSecret func()
}

func (s *mutableCredentialResourceStore) List(context.Context) ([]connectorapi.CredentialResource, error) {
	return []connectorapi.CredentialResource{s.record}, nil
}

func (s *mutableCredentialResourceStore) Get(context.Context, int64) (connectorapi.CredentialResource, error) {
	return s.record, nil
}

func (s *mutableCredentialResourceStore) GetSecret(_ context.Context, _ int64, destination any) error {
	target, ok := destination.(*map[string]any)
	if !ok {
		return errors.New("unexpected secret destination")
	}
	*target = s.secret
	if s.afterSecret != nil {
		s.afterSecret()
	}
	return nil
}

func (s *mutableCredentialResourceStore) Create(context.Context, connectorapi.CreateCredentialResourceInput) (connectorapi.CredentialResource, error) {
	return s.record, nil
}

func (s *mutableCredentialResourceStore) Update(context.Context, int64, connectorapi.UpdateCredentialResourceInput) (connectorapi.CredentialResource, error) {
	return s.record, nil
}

func (*mutableCredentialResourceStore) Delete(context.Context, int64) error { return nil }

func (*mutableCredentialResourceStore) CountProfileReferences(context.Context, string, int64) (int, error) {
	return 0, nil
}

func TestBoundCredentialResourceStoreRedactsAndRejectsMutation(t *testing.T) {
	boundary := actionresult.NewCredentialBoundary(nil)
	delegate := &mutableCredentialResourceStore{
		record: connectorapi.CredentialResource{ID: 7, Name: "key", UpdatedAt: "one"},
		secret: map[string]any{"private_key": "secret-private-key"},
	}
	store := boundCredentialResourceStore{
		delegate: delegate,
		kind:     "private_key",
		bindings: newCredentialResourceBindings(boundary),
	}

	var secret map[string]any
	if err := store.GetSecret(t.Context(), 7, &secret); err != nil {
		t.Fatalf("read bound secret: %v", err)
	}
	if got := boundary.Redact("prefix secret-private-key suffix"); got == "prefix secret-private-key suffix" {
		t.Fatal("decrypted credential resource was not added to the redaction boundary")
	}
	if _, err := store.Create(t.Context(), connectorapi.CreateCredentialResourceInput{}); !errors.Is(err, errTransferCredentialResourcesReadOnly) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := store.Update(t.Context(), 7, connectorapi.UpdateCredentialResourceInput{}); !errors.Is(err, errTransferCredentialResourcesReadOnly) {
		t.Fatalf("update error = %v", err)
	}
	if err := store.Delete(t.Context(), 7); !errors.Is(err, errTransferCredentialResourcesReadOnly) {
		t.Fatalf("delete error = %v", err)
	}
}

func TestBoundCredentialResourceStoreRejectsRevisionDrift(t *testing.T) {
	delegate := &mutableCredentialResourceStore{
		record: connectorapi.CredentialResource{ID: 7, Name: "key", UpdatedAt: "one"},
	}
	store := boundCredentialResourceStore{
		delegate: delegate,
		kind:     "private_key",
		bindings: newCredentialResourceBindings(actionresult.NewCredentialBoundary(nil)),
	}
	if _, err := store.Get(t.Context(), 7); err != nil {
		t.Fatalf("bind initial resource: %v", err)
	}
	delegate.record.UpdatedAt = "two"
	if _, err := store.Get(t.Context(), 7); !errors.Is(err, errTransferExecutionStale) {
		t.Fatalf("changed resource error = %v", err)
	}
}

func TestBoundCredentialResourceStoreRejectsDriftDuringDecryptAndStillRedacts(t *testing.T) {
	boundary := actionresult.NewCredentialBoundary(nil)
	delegate := &mutableCredentialResourceStore{
		record: connectorapi.CredentialResource{ID: 7, Name: "key", UpdatedAt: "one"},
		secret: map[string]any{"private_key": "decrypted-private-key"},
	}
	delegate.afterSecret = func() { delegate.record.UpdatedAt = "two" }
	store := boundCredentialResourceStore{
		delegate: delegate,
		kind:     "private_key",
		bindings: newCredentialResourceBindings(boundary),
	}

	var secret map[string]any
	if err := store.GetSecret(t.Context(), 7, &secret); !errors.Is(err, errTransferExecutionStale) {
		t.Fatalf("decrypt-time drift error = %v", err)
	}
	if got := boundary.Redact("decrypted-private-key"); got == "decrypted-private-key" {
		t.Fatal("secret from a stale decrypt was not redacted")
	}
}
