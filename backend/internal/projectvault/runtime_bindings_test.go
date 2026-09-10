package projectvault

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestRuntimeOwnsDefaultBindingLifecycle(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	item := harness.create(t)
	targetStore := connectortargets.NewStore(harness.database)
	target, err := targetStore.CreateTarget(t.Context(), connectortargets.CreateTargetInput{
		ConnectorKind: "test", Name: "runtime-binding-target",
		Config: map[string]any{"endpoint": "local"},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := targetStore.CreateCredentialProfile(t.Context(), connectortargets.CreateCredentialProfileInput{
		TargetID: target.ID, ConnectorKind: target.ConnectorKind,
		Kind: "test", Label: "default", Public: map[string]any{"identity": "runtime"},
	})
	if err != nil {
		t.Fatal(err)
	}
	input := DefaultBindingInput{
		VaultItemID: item.ID, SourceProjectID: harness.projectID,
		TargetID: target.ID, ProfileID: profile.ID,
	}
	created, err := harness.runtime.SaveDefaultBinding(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if created.BindingRevision != 1 || harness.bindingTargets.calls != 1 {
		t.Fatalf("created binding = %#v validation calls=%d", created, harness.bindingTargets.calls)
	}
	auditCount := len(harness.mutations.actions)
	invalidationCount := harness.invalidated
	input.ExpectedBindingRevision = created.BindingRevision
	unchanged, err := harness.runtime.SaveDefaultBinding(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.BindingRevision != created.BindingRevision || len(harness.mutations.actions) != auditCount || harness.invalidated != invalidationCount {
		t.Fatalf("no-op binding = %#v audits=%#v invalidations=%d", unchanged, harness.mutations.actions, harness.invalidated)
	}
	input.ReplaceExisting = true
	updated, err := harness.runtime.SaveDefaultBinding(t.Context(), input)
	if err != nil {
		t.Fatal(err)
	}
	if updated.BindingRevision != 2 || !updated.ReplaceExisting {
		t.Fatalf("updated binding = %#v", updated)
	}
	listed, err := harness.runtime.ListDefaultBindings(t.Context(), DefaultBindingFilter{VaultItemID: item.ID})
	if err != nil || len(listed) != 1 || listed[0].ID != updated.ID {
		t.Fatalf("listed bindings = %#v error=%v", listed, err)
	}
	if err := harness.runtime.DeleteDefaultBinding(t.Context(), updated.ID, created.BindingRevision); !errors.Is(err, ErrStale) {
		t.Fatalf("stale delete error = %v", err)
	}
	if err := harness.runtime.DeleteDefaultBinding(t.Context(), updated.ID, updated.BindingRevision); err != nil {
		t.Fatal(err)
	}
	if got := harness.mutations.actions[len(harness.mutations.actions)-1]; got != "vault.binding.deleted" {
		t.Fatalf("last audit action = %q", got)
	}
}

func TestRuntimeRejectsBindingBeforePersistenceWhenTargetValidationFails(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	harness.bindingTargets.err = ErrSessionEnvironmentUnsupported
	_, err := harness.runtime.SaveDefaultBinding(t.Context(), DefaultBindingInput{
		VaultItemID: 1, SourceProjectID: harness.projectID, TargetID: 1, ProfileID: 1,
	})
	if !errors.Is(err, ErrSessionEnvironmentUnsupported) {
		t.Fatalf("SaveDefaultBinding() error = %v", err)
	}
	var count int
	if queryErr := harness.database.QueryRow(`SELECT COUNT(*) FROM vault_default_bindings`).Scan(&count); queryErr != nil || count != 0 {
		t.Fatalf("persisted bindings=%d query error=%v", count, queryErr)
	}
}
