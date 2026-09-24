package projectvault

import (
	"context"
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func TestReplaceValueKeepsCommittedChangePendingWhenInvalidationFails(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	item := harness.create(t)
	harness.runtime.invalidateSessions = func(context.Context, []SessionReference, SessionMutationScope) error {
		return errors.New("injected invalidation failure")
	}
	replaced, err := harness.runtime.ReplaceValue(t.Context(), ReplaceRuntimeValueInput{
		ID: item.ID, Value: "new-value", Source: "imported", ExpectedValueVersion: item.ValueVersion,
	})
	if !errors.Is(err, vaultfinalization.ErrPending) || replaced.ValueVersion != item.ValueVersion+1 {
		t.Fatalf("replacement = %#v, error = %v", replaced, err)
	}
	value, err := harness.runtime.store.Reveal(t.Context(), item.ID)
	if err != nil || value != "new-value" {
		t.Fatalf("committed value = %q, error = %v", value, err)
	}
	finalizations := vaultfinalization.NewStore(harness.database)
	if err := finalizations.RequireReady(t.Context()); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("readiness = %v", err)
	}
	if _, err := harness.runtime.Create(t.Context(), CreateInput{OwnerProjectID: harness.projectID, Name: "OTHER_SECRET", Value: "x"}); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("mutation while cleanup is pending = %v", err)
	}
	intents, err := finalizations.Pending(t.Context())
	if err != nil || len(intents) != 1 || intents[0].ItemID != item.ID {
		t.Fatalf("pending intents = %#v, error = %v", intents, err)
	}
	if err := finalizations.Finalize(t.Context(), intents[0].ID, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if err := finalizations.RequireReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}
