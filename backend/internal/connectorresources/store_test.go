package connectorresources

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestScopedStoreOwnsEncryptedCredentialResourceLifecycle(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "resources.db"), "ResourcePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secretVault, err := vault.New("ResourcePassword123")
	if err != nil {
		t.Fatal(err)
	}
	scope := NewStore(database, secretVault, "workspace-resources").Scope("ssh", "private_key")
	created, err := scope.Create(t.Context(), connectorapi.CreateCredentialResourceInput{
		Name: "operator", ResourceType: "ed25519", PublicData: "ssh-ed25519 public", Fingerprint: "SHA256:fixture",
		Secret: map[string]any{"private_key": "private-material"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var secret map[string]any
	if err := scope.GetSecret(t.Context(), created.ID, &secret); err != nil || secret["private_key"] != "private-material" {
		t.Fatalf("credential secret did not round-trip: %v", err)
	}
	listed, err := scope.List(t.Context())
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("listed resources = %#v, err=%v", listed, err)
	}
	updated, err := scope.Update(t.Context(), created.ID, connectorapi.UpdateCredentialResourceInput{Name: "operator-renamed", PublicData: "updated-public"})
	if err != nil || updated.Name != "operator-renamed" || updated.PublicData != "updated-public" {
		t.Fatalf("updated resource = %#v, err=%v", updated, err)
	}

	targetStore := connectortargets.NewStore(database)
	target, err := targetStore.CreateTarget(t.Context(), connectortargets.CreateTargetInput{ConnectorKind: "ssh", Name: "server", Config: map[string]any{"host": "127.0.0.1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := targetStore.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "ssh", Kind: "private_key", Label: "root",
		Public: map[string]any{"ssh_key_id": created.ID},
	}); err != nil {
		t.Fatal(err)
	}
	references, err := scope.CountProfileReferences(t.Context(), "ssh_key_id", created.ID)
	if err != nil || references != 1 {
		t.Fatalf("profile references = %d, err=%v", references, err)
	}
	if err := scope.Delete(t.Context(), created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := scope.Get(context.Background(), created.ID); !errors.Is(err, connectorapi.ErrCredentialResourceNotFound) {
		t.Fatalf("deleted resource error = %v", err)
	}
}

func TestScopedStoreFailsClosedWithoutScope(t *testing.T) {
	scope := (&Store{}).Scope("", "")
	if _, err := scope.List(t.Context()); err == nil {
		t.Fatal("invalid resource scope was accepted")
	}
}
