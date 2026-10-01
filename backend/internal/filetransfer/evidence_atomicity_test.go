package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestLateTransferEvidencePreservesCanceledStateAndBatchTotals(t *testing.T) {
	database, store, runtimeID := newAtomicTransferStore(t)
	batch := createAtomicBatch(t, store, runtimeID, 2)
	if changed, err := store.CancelBatch(t.Context(), batch.ID, "user canceled"); err != nil || !changed {
		t.Fatalf("cancel: changed=%v err=%v", changed, err)
	}
	before, err := store.Get(t.Context(), batch.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, bytes := range []int64{6, 4, 6} {
		if err := store.UpdateEvidence(t.Context(), before.ID, bytes, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpdateEvidence(t.Context(), batch.Items[1].ID, 3, ""); err != nil {
		t.Fatal(err)
	}
	after, err := store.Get(t.Context(), before.ID)
	if err != nil || after.Status != before.Status || after.CompletedAt != before.CompletedAt || after.Error != before.Error || after.FailureKind != before.FailureKind || after.TransferredBytes != 6 {
		t.Fatalf("late evidence changed terminal state: before=%#v after=%#v err=%v", before, after, err)
	}
	stored, err := store.GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != StatusCanceled || stored.TransferredBytes != 9 || stored.CanceledItems != 2 {
		t.Fatalf("late evidence did not atomically repair totals: batch=%#v err=%v", stored, err)
	}
	assertEvidenceHistoryBytes(t, database, before.ID, 6)
}

func TestTransferEvidenceRollsBackWithHistoryOrBatchProjection(t *testing.T) {
	for _, projection := range []string{"history", "batch"} {
		t.Run(projection, func(t *testing.T) {
			database, store, runtimeID := newAtomicTransferStore(t)
			batch := createAtomicBatch(t, store, runtimeID, 1)
			if projection == "history" {
				installHistoryProjectionFailure(t, database, "INSERT")
			} else if _, err := database.Exec(`CREATE TRIGGER reject_batch_evidence BEFORE UPDATE ON file_transfer_batches
				BEGIN SELECT RAISE(ABORT, 'injected batch evidence failure'); END`); err != nil {
				t.Fatal(err)
			}
			if err := store.UpdateEvidence(t.Context(), batch.Items[0].ID, 6, "hash"); err == nil {
				t.Fatal("injected projection failure did not roll back")
			}
			item, err := store.Get(t.Context(), batch.Items[0].ID)
			if err != nil || item.TransferredBytes != 0 || item.ChecksumSHA256 != "" {
				t.Fatalf("failed evidence partially committed: item=%#v err=%v", item, err)
			}
			assertEvidenceHistoryBytes(t, database, item.ID, 0)
			stored, err := store.GetBatch(t.Context(), batch.ID)
			if err != nil || stored.TransferredBytes != 0 {
				t.Fatalf("failed evidence changed batch: batch=%#v err=%v", stored, err)
			}
			if projection == "history" {
				dropHistoryProjectionFailure(t, database)
			} else if _, err := database.Exec(`DROP TRIGGER reject_batch_evidence`); err != nil {
				t.Fatal(err)
			}
			if err := store.UpdateEvidence(t.Context(), item.ID, 6, "hash"); err != nil {
				t.Fatal(err)
			}
			assertEvidenceHistoryBytes(t, database, item.ID, 6)
			stored, err = store.GetBatch(t.Context(), batch.ID)
			if err != nil || stored.TransferredBytes != 6 {
				t.Fatalf("retry did not repair totals: batch=%#v err=%v", stored, err)
			}
		})
	}
}

func TestTransferEvidenceRejectsMissingAndInvalidRecords(t *testing.T) {
	_, store, _ := newAtomicTransferStore(t)
	if err := store.UpdateEvidence(t.Context(), 999, 6, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing record falsely durable: %v", err)
	}
	for _, input := range []struct{ id, bytes int64 }{{0, 1}, {1, -1}} {
		if err := store.UpdateEvidence(t.Context(), input.id, input.bytes, ""); !errors.Is(err, ErrInvalidArgument) {
			t.Fatalf("invalid evidence accepted: %v", err)
		}
	}
}

func TestLateTransferEvidenceSurvivesEncryptedReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "evidence.aipdb")
	password := "EvidenceReopenPassword123"
	database, err := dbpkg.OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewStore(database)
	batch := createAtomicBatch(t, store, insertTestServer(t, database), 1)
	if _, err := store.CancelBatch(t.Context(), batch.ID, "canceled"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateEvidence(t.Context(), batch.Items[0].ID, 6, ""); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = dbpkg.OpenEncrypted(path, password)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := NewStore(database).GetBatch(t.Context(), batch.ID)
	if err != nil || stored.Status != StatusCanceled || stored.TransferredBytes != 6 || len(stored.Items) != 1 || stored.Items[0].TransferredBytes != 6 {
		t.Fatalf("reopen lost late evidence: batch=%#v err=%v", stored, err)
	}
	assertEvidenceHistoryBytes(t, database, batch.Items[0].ID, 6)
}

func assertEvidenceHistoryBytes(t *testing.T, database *sql.DB, id, want int64) {
	t.Helper()
	var bytes int64
	if err := database.QueryRowContext(context.Background(), `SELECT bytes_done FROM history_entries
		WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, id).Scan(&bytes); err != nil {
		t.Fatal(err)
	}
	if bytes != want {
		t.Fatalf("history bytes=%d want=%d", bytes, want)
	}
}
