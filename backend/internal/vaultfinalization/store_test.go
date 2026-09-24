package vaultfinalization_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vaultfinalization"
)

func TestIntentRollsBackWithMutation(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "vault.aipdb"), "VaultFinalizationPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = vaultfinalization.NewStore(tx).Queue(t.Context(), vaultfinalization.Intent{Kind: "mutation", ItemID: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := vaultfinalization.NewStore(database).RequireReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestFinalizeCompletesAfterRequestCancellation(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "cancel.aipdb"), "VaultFinalizationPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := vaultfinalization.NewStore(database)
	id, err := store.Queue(t.Context(), vaultfinalization.Intent{Kind: "project", ProjectID: 9})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := store.Finalize(ctx, id, func(cleanupCtx context.Context) error {
		return cleanupCtx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestPendingIntentBlocksDeliveryUntilCompleted(t *testing.T) {
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "vault.aipdb"), "VaultFinalizationPassword123")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := vaultfinalization.NewStore(database)
	id, err := store.Queue(t.Context(), vaultfinalization.Intent{
		Kind: "mutation", ItemID: 3, References: []vaultfinalization.Reference{{SessionID: 8, RuntimeID: 4, Generation: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); !errors.Is(err, vaultfinalization.ErrBlocked) {
		t.Fatalf("readiness = %v", err)
	}
	intents, err := store.Pending(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(intents) != 1 || intents[0].ID != id || len(intents[0].References) != 1 || intents[0].References[0].SessionID != 8 {
		t.Fatalf("pending = %#v", intents)
	}
	if err := store.Complete(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if err := store.RequireReady(t.Context()); err != nil {
		t.Fatal(err)
	}
}
