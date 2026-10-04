package uploadoperation_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/backups/uploadoperation"
	"github.com/aipermission/aipermission/backend/internal/db"
)

func journalClaim(t *testing.T) (*sql.DB, *uploadoperation.Store, uploadoperation.ClaimRequest) {
	t.Helper()
	database, err := db.OpenEncrypted(filepath.Join(t.TempDir(), "journal.aipdb"), "StrongDatabasePassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	result, err := database.ExecContext(t.Context(), `INSERT INTO backup_providers (provider_type, name, created_at, updated_at) VALUES ('aipermission_backup', 'Journal contract', '2026-10-04T00:00:00Z', '2026-10-04T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	providerID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return database, uploadoperation.NewStore(database), uploadoperation.ClaimRequest{
		IdempotencyKey: "journal-key", ProviderID: providerID, DatabaseID: "display-name",
		WorkspaceInstanceID: "instance", StreamID: "stream", SourceInstallationID: "installation",
	}
}

func TestUploadJournalIdentityDoesNotMutateExistingClaim(t *testing.T) {
	_, store, request := journalClaim(t)
	before, created, err := store.Claim(t.Context(), request)
	if err != nil || !created {
		t.Fatalf("claim: created=%v err=%v", created, err)
	}
	for _, change := range []func(*uploadoperation.ClaimRequest){
		func(r *uploadoperation.ClaimRequest) { r.ProviderID++ },
		func(r *uploadoperation.ClaimRequest) { r.WorkspaceInstanceID += "-copy" },
		func(r *uploadoperation.ClaimRequest) { r.StreamID += "-other" },
		func(r *uploadoperation.ClaimRequest) { r.SourceInstallationID += "-other" },
	} {
		changed := request
		change(&changed)
		if _, _, err := store.Claim(t.Context(), changed); !errors.Is(err, uploadoperation.ErrIdempotencyConflict) {
			t.Fatalf("changed identity accepted: %v", err)
		}
		after, err := store.Get(t.Context(), request.IdempotencyKey)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("conflict changed durable claim: %#v err=%v", after, err)
		}
	}
	request.DatabaseID = "renamed-display"
	after, created, err := store.Claim(t.Context(), request)
	if err != nil || created || !reflect.DeepEqual(before, after) {
		t.Fatalf("renamed replay: %#v created=%v err=%v", after, created, err)
	}
}

func TestUploadJournalTerminalCompletionCannotBeOverwritten(t *testing.T) {
	_, store, request := journalClaim(t)
	if _, _, err := store.Claim(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkDispatched(t.Context(), request.IdempotencyKey); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkOutcomeUnknown(t.Context(), request.IdempotencyKey, errors.New("reply lost")); err != nil {
		t.Fatal(err)
	}
	if err := store.Complete(t.Context(), request.IdempotencyKey, "remote-result"); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(t.Context(), request.IdempotencyKey)
	if err != nil || before.Status != "completed" || before.CompletedAt == nil || before.LastError != "" || before.ProviderFileID != "remote-result" {
		t.Fatalf("completion: %#v err=%v", before, err)
	}
	for _, replay := range []func() error{
		func() error { return store.MarkDispatched(t.Context(), request.IdempotencyKey) },
		func() error { return store.MarkOutcomeUnknown(t.Context(), request.IdempotencyKey, nil) },
		func() error { return store.MarkExpired(t.Context(), request.IdempotencyKey) },
		func() error { return store.Complete(t.Context(), request.IdempotencyKey, "remote-result") },
	} {
		if err := replay(); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Complete(t.Context(), request.IdempotencyKey, "different-result"); err == nil {
		t.Fatal("terminal identity replaced")
	}
	after, err := store.Get(t.Context(), request.IdempotencyKey)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("terminal replay mutated durable state: %#v err=%v", after, err)
	}
}

func TestUploadJournalMutationUsesCallerTransaction(t *testing.T) {
	database, _, request := journalClaim(t)
	tx, err := database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	journal := uploadoperation.NewStore(tx)
	if _, _, err := journal.Claim(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	if err := journal.Complete(t.Context(), request.IdempotencyKey, "remote-result"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := uploadoperation.NewStore(database).Get(t.Context(), request.IdempotencyKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rolled back journal is visible: %v", err)
	}
	if _, created, err := uploadoperation.NewStore(database).Claim(t.Context(), request); err != nil || !created {
		t.Fatalf("healthy claim after proven rollback: created=%v err=%v", created, err)
	}
}
