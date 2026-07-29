package projectvault

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
)

func TestDefaultBindingsValidateAssignmentsAndUseOptimisticRevision(t *testing.T) {
	ctx := context.Background()
	database, store := openTestStore(t)
	projects := projectstore.NewStore(database)
	owner, err := projects.Create(ctx, "Owner")
	if err != nil {
		t.Fatalf("create owner project: %v", err)
	}
	shared, err := projects.Create(ctx, "Shared")
	if err != nil {
		t.Fatalf("create shared project: %v", err)
	}
	unassigned, err := projects.Create(ctx, "Unassigned")
	if err != nil {
		t.Fatalf("create unassigned project: %v", err)
	}
	item, err := store.Create(ctx, CreateInput{
		Name: "BINDING_TEST_SECRET", Value: "binding-test-secret-value",
		OwnerProjectID: owner.ID, SharedProjectIDs: []int64{shared.ID},
		SecretType: "generic_secret", Source: "imported", ExpiryWarningDays: 14,
	})
	if err != nil {
		t.Fatalf("create vault item: %v", err)
	}
	targets := connectortargets.NewStore(database)
	target, err := targets.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: "ssh", Name: "binding-target",
		Config: map[string]any{"host": "127.0.0.1", "port": 22},
	})
	if err != nil {
		t.Fatalf("create target: %v", err)
	}
	profile, err := targets.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: "ssh", Kind: "private_key", Label: "admin",
		Public: map[string]any{"username": "root"},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}

	if _, err := store.SaveDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: unassigned.ID,
		TargetID: target.ID, ProfileID: profile.ID,
	}); err == nil {
		t.Fatal("unassigned source project should fail")
	}
	created, err := store.SaveDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: shared.ID,
		TargetID: target.ID, ProfileID: profile.ID,
	})
	if err != nil {
		t.Fatalf("create default binding: %v", err)
	}
	if created.BindingRevision != 1 || created.ReplaceExisting {
		t.Fatalf("unexpected created binding: %#v", created)
	}
	updated, err := store.SaveDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: shared.ID,
		TargetID: target.ID, ProfileID: profile.ID, ReplaceExisting: true,
		ExpectedBindingRevision: created.BindingRevision,
	})
	if err != nil {
		t.Fatalf("update default binding: %v", err)
	}
	if updated.BindingRevision != 2 || !updated.ReplaceExisting {
		t.Fatalf("unexpected updated binding: %#v", updated)
	}
	found, ok, err := store.FindDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: shared.ID,
		TargetID: target.ID, ProfileID: profile.ID,
	})
	if err != nil || !ok || found.ID != updated.ID || found.BindingRevision != updated.BindingRevision {
		t.Fatalf("find binding = %#v, %v, %v", found, ok, err)
	}
	if _, ok, err := store.FindDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: owner.ID,
		TargetID: target.ID, ProfileID: profile.ID,
	}); err != nil || ok {
		t.Fatalf("unexpected binding lookup = %v, %v", ok, err)
	}
	if _, err := store.SaveDefaultBinding(ctx, DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: shared.ID,
		TargetID: target.ID, ProfileID: profile.ID,
		ExpectedBindingRevision: created.BindingRevision,
	}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale update = %v", err)
	}
	items, err := store.ListDefaultBindings(ctx, item.ID, 0, 0)
	if err != nil || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("list bindings = %#v, %v", items, err)
	}
	if err := store.DeleteDefaultBinding(ctx, created.ID, created.BindingRevision); !errors.Is(err, ErrStale) {
		t.Fatalf("stale delete = %v", err)
	}
	if err := store.DeleteDefaultBinding(ctx, created.ID, updated.BindingRevision); err != nil {
		t.Fatalf("delete binding: %v", err)
	}
}
