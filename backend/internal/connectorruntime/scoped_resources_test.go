package connectorruntime

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	appdb "github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func openScopedResourceFixture(t *testing.T, path string) (*sql.DB, ResourceScopes) {
	t.Helper()
	database, err := appdb.OpenEncrypted(path, "scoped-resources-fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("scoped-resources-fixture-secret")
	if err != nil {
		t.Fatal(err)
	}
	return database, NewResourceScopes(database, secretVault, "scoped-resources-fixture-workspace")
}

func TestScopedResourceRuntimeKeepsConnectorAndResourceClassBoundaries(t *testing.T) {
	_, scopes := openScopedResourceFixture(t, filepath.Join(t.TempDir(), "scoped.aipdb"))
	first := NewScope("first", Dependencies{Resources: scopes}).ScopedResourceRuntime()
	second := NewScope("second", Dependencies{Resources: scopes}).ScopedResourceRuntime()
	journal := first.CredentialResources("domain_journal")
	row, err := journal.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
		Name: "owned-record", ResourceType: "domain.v1", PublicData: `{"status":"intent"}`,
		Fingerprint: "fixture-identity", Secret: map[string]any{"proof": "fixture-proof-only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, foreign := range []connectorapi.CredentialResourceStore{
		second.CredentialResources("domain_journal"), first.CredentialResources("another_class"),
	} {
		if rows, err := foreign.List(t.Context()); err != nil || len(rows) != 0 {
			t.Fatalf("foreign scope listed owned journal: %#v err=%v", rows, err)
		}
		if _, err := foreign.Get(t.Context(), row.ID); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) {
			t.Fatalf("foreign scope read journal: %v", err)
		}
		var secret map[string]any
		if err := foreign.GetSecret(t.Context(), row.ID, &secret); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) || len(secret) != 0 {
			t.Fatalf("foreign scope decrypted journal: %#v err=%v", secret, err)
		}
		if _, err := foreign.Update(t.Context(), row.ID, connectorapi.UpdateCredentialResourceInput{Name: row.Name, PublicData: `{}`}); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) {
			t.Fatalf("foreign scope mutated journal: %v", err)
		}
		if err := foreign.Delete(t.Context(), row.ID); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) {
			t.Fatalf("foreign scope deleted journal: %v", err)
		}
	}
	current, err := journal.Get(t.Context(), row.ID)
	if err != nil || !reflect.DeepEqual(current, row) {
		t.Fatalf("foreign attempts changed owned record: %#v err=%v", current, err)
	}
}

func TestScopedResourceRuntimeRecoversPersistedStateAfterEncryptedReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.aipdb")
	database, scopes := openScopedResourceFixture(t, path)
	journal := NewScope("fixture", Dependencies{Resources: scopes}).ScopedResourceRuntime().CredentialResources("domain_journal")
	row, err := journal.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
		Name: "pending-operation", ResourceType: "domain.v1", PublicData: `{"status":"intent"}`,
		Fingerprint: "fixture-identity", Secret: map[string]any{"proof": "fixture-proof-only"},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err = journal.Update(t.Context(), row.ID, connectorapi.UpdateCredentialResourceInput{
		Name: row.Name, PublicData: `{"status":"confirmed"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	_, reopened := openScopedResourceFixture(t, path)
	store := NewScope("fixture", Dependencies{Resources: reopened}).ScopedResourceRuntime().CredentialResources("domain_journal")
	current, err := store.Get(t.Context(), row.ID)
	if err != nil || !reflect.DeepEqual(current, row) {
		t.Fatalf("persisted journal changed on reopen: %#v err=%v", current, err)
	}
	var secret map[string]any
	if err := store.GetSecret(t.Context(), row.ID, &secret); err != nil || secret["proof"] != "fixture-proof-only" {
		t.Fatalf("encrypted journal payload was not recoverable: %#v err=%v", secret, err)
	}
}

func TestScopedResourceRuntimeRejectsMissingBackingAuthority(t *testing.T) {
	for _, scope := range []*Scope{nil, NewScope("fixture", Dependencies{}), NewScope("", Dependencies{})} {
		if scope.ScopedResourceRuntime().CredentialResources("domain_journal") != nil {
			t.Fatal("missing scope exposed resource storage")
		}
	}
	_, resources := openScopedResourceFixture(t, filepath.Join(t.TempDir(), "invalid-kind.aipdb"))
	scope := NewScope("fixture", Dependencies{Resources: resources}).ScopedResourceRuntime()
	for _, kind := range []string{"", " ", "\t"} {
		if scope.CredentialResources(kind) != nil {
			t.Fatalf("blank resource kind %q exposed storage", kind)
		}
	}
}
