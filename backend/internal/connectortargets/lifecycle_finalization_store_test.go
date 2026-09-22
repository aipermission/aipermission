package connectortargets_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreQueuesCoalescesAndCompletesFinalization(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "lifecycle.aipdb"), "LifecycleStorePassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := connectortargets.NewLifecycleFinalizationStore(database)
	change := connectortargets.LifecycleFinalizationChange{
		TargetID: 7, ProfileID: 3, StaleReason: "stale", UserMessage: "refresh",
	}
	if err := store.Queue(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	change.IncludeRunning = true
	change.UserMessage = "latest"
	if err := store.Queue(t.Context(), change); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); !errors.Is(err, connectortargets.ErrLifecycleFinalizationPending) {
		t.Fatalf("readiness error = %v", err)
	}
	items, err := store.Pending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].Change.IncludeRunning || items[0].Change.UserMessage != "latest" {
		t.Fatalf("pending finalization = %#v", items)
	}
	if err := store.RecordAttempt(t.Context(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkVaultComplete(t.Context(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); !errors.Is(err, connectortargets.ErrLifecycleFinalizationPending) {
		t.Fatalf("request-pending readiness error = %v", err)
	}
	if err := store.MarkRequestsComplete(t.Context(), items[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); err != nil {
		t.Fatalf("completed readiness error = %v", err)
	}
}

func TestStoreQueueRollsBackWithOwningMutation(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "rollback.aipdb"), "LifecycleRollbackPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := connectortargets.NewLifecycleFinalizationStore(tx).Queue(t.Context(), connectortargets.LifecycleFinalizationChange{TargetID: 8}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := connectortargets.NewLifecycleFinalizationStore(database).RequireReady(t.Context()); err != nil {
		t.Fatalf("rolled-back lifecycle intent remained visible: %v", err)
	}
}
