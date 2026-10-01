package filetransfer

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestFailActiveRecalculatesPendingBatchAndPersistsSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shutdown.db")
	database, err := dbpkg.OpenEncrypted(path, "ShutdownTransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := NewStore(database)
	batch := createAtomicBatch(t, store, insertTestServer(t, database), 1)
	if err := store.FailActive(t.Context(), "stopped", "stopped"); err != nil {
		t.Fatal(err)
	}
	assertShutdownBatchSummary(t, store, batch.ID, 1, 0, 1, 0, 0)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := dbpkg.OpenEncrypted(path, "ShutdownTransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	assertShutdownBatchSummary(t, NewStore(reopened), batch.ID, 1, 0, 1, 0, 0)
}

func TestFailActivePreservesCompletedAndCanceledCounts(t *testing.T) {
	_, store, runtimeID := newAtomicTransferStore(t)
	batch := createAtomicBatch(t, store, runtimeID, 4)
	if changed, err := store.MarkBatchRunning(t.Context(), batch.ID); err != nil || !changed {
		t.Fatalf("mark batch running: changed=%v err=%v", changed, err)
	}
	for _, index := range []int{0, 2} {
		markAtomicTransferRunning(t, store, batch.Items[index].ID)
	}
	if changed, err := store.Complete(t.Context(), batch.Items[0].ID, 12, "checksum"); err != nil || !changed {
		t.Fatalf("complete transfer: changed=%v err=%v", changed, err)
	}
	if changed, err := store.Cancel(t.Context(), batch.Items[1].ID, "canceled"); err != nil || !changed {
		t.Fatalf("cancel transfer: changed=%v err=%v", changed, err)
	}
	if err := store.UpdateProgressStats(t.Context(), batch.Items[2].ID, 5, 100, 25, 3); err != nil {
		t.Fatal(err)
	}
	if err := store.RecalculateBatch(t.Context(), batch.ID); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetBatch(t.Context(), batch.ID)
	if err != nil || before.BytesPerSecond != 25 {
		t.Fatalf("active summary: batch=%#v err=%v", before, err)
	}
	if err := store.FailActive(t.Context(), "stopped", "stopped"); err != nil {
		t.Fatal(err)
	}
	assertShutdownBatchSummary(t, store, batch.ID, 4, 1, 2, 1, 17)
	terminal, err := store.GetBatch(t.Context(), batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.SizeBytes != before.SizeBytes || terminal.SizeBytes < 100 {
		t.Fatalf("shutdown lost total size: before=%d after=%d", before.SizeBytes, terminal.SizeBytes)
	}
	if err := store.FailActive(t.Context(), "retry", "retry"); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetBatch(t.Context(), batch.ID)
	if err != nil || after.UpdatedAt != terminal.UpdatedAt || after.Error != terminal.Error || after.CompletedAt != terminal.CompletedAt {
		t.Fatalf("idempotent shutdown: batch=%#v err=%v", after, err)
	}
	assertShutdownBatchSummary(t, store, batch.ID, 4, 1, 2, 1, 17)
}

func TestFailActiveAggregateOrHistoryFailureRollsBack(t *testing.T) {
	for _, point := range []string{"aggregate", "history"} {
		t.Run(point, func(t *testing.T) {
			database, store, runtimeID := newAtomicTransferStore(t)
			batch := createAtomicBatch(t, store, runtimeID, 1)
			historyBefore := shutdownHistorySnapshot(t, database)
			if point == "aggregate" {
				if _, err := database.Exec(`CREATE TRIGGER reject_shutdown_aggregate BEFORE UPDATE OF total_items ON file_transfer_batches
					BEGIN SELECT RAISE(ABORT, 'injected aggregate failure'); END`); err != nil {
					t.Fatal(err)
				}
			} else {
				installHistoryProjectionFailure(t, database, "INSERT")
			}
			if err := store.FailActive(t.Context(), "stopped", "stopped"); err == nil {
				t.Fatal("shutdown committed despite failed derived update")
			}
			assertAtomicBatchStatus(t, store, batch.ID, StatusPending, StatusPending)
			unchanged, err := store.GetBatch(t.Context(), batch.ID)
			if err != nil || unchanged.FailedItems != 0 || unchanged.CompletedAt != "" {
				t.Fatalf("partial summary after rollback: batch=%#v err=%v", unchanged, err)
			}
			if historyAfter := shutdownHistorySnapshot(t, database); historyAfter != historyBefore {
				t.Fatalf("shutdown failure changed history: before=%s after=%s", historyBefore, historyAfter)
			}
			if point == "aggregate" {
				if _, err := database.Exec(`DROP TRIGGER reject_shutdown_aggregate`); err != nil {
					t.Fatal(err)
				}
			} else {
				dropHistoryProjectionFailure(t, database)
			}
			if err := store.FailActive(t.Context(), "stopped", "stopped"); err != nil {
				t.Fatal(err)
			}
			assertShutdownBatchSummary(t, store, batch.ID, 1, 0, 1, 0, 0)
		})
	}
}

func shutdownHistorySnapshot(t *testing.T, database *sql.DB) string {
	t.Helper()
	var snapshot string
	err := database.QueryRow(`SELECT COALESCE(json_group_array(json_object(
		'id', id, 'status', status, 'preview', preview_json, 'error', error,
		'created', created_at, 'updated', updated_at, 'completed', completed_at)), '[]')
		FROM (SELECT * FROM history_entries WHERE source_ref_type = 'file_transfer' ORDER BY id)`).Scan(&snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestFailActiveRecalculatesEveryActiveBatchState(t *testing.T) {
	for _, status := range []string{StatusPendingApproval, StatusPending, StatusRunning, StatusPaused} {
		t.Run(status, func(t *testing.T) {
			database, store, runtimeID := newAtomicTransferStore(t)
			batch := createAtomicBatch(t, store, runtimeID, 1)
			if _, err := database.Exec(`UPDATE file_transfer_batches SET status = ? WHERE id = ?`, status, batch.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE file_transfers SET status = ? WHERE batch_id = ?`, status, batch.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.FailActive(t.Context(), "stopped", "stopped"); err != nil {
				t.Fatal(err)
			}
			assertShutdownBatchSummary(t, store, batch.ID, 1, 0, 1, 0, 0)
		})
	}
}

func TestFailActiveOnlyRecalculatesAffectedBatches(t *testing.T) {
	database, store, runtimeID := newAtomicTransferStore(t)
	untouched := createAtomicBatch(t, store, runtimeID, 1)
	if changed, err := store.CancelBatch(t.Context(), untouched.ID, "original"); err != nil || !changed {
		t.Fatalf("cancel unrelated batch: changed=%v err=%v", changed, err)
	}
	if _, err := database.Exec(`UPDATE file_transfer_batches SET updated_at = '2000-01-01T00:00:00Z' WHERE id = ?`, untouched.ID); err != nil {
		t.Fatal(err)
	}
	// Recovery may encounter a terminal parent with an interrupted child.
	affected := createAtomicBatch(t, store, runtimeID, 1)
	if _, err := database.Exec(`UPDATE file_transfer_batches SET status = ?, error = 'original', completed_at = '2000-01-01T00:00:00Z' WHERE id = ?`, StatusFailed, affected.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FailActive(t.Context(), "stopped", "stopped"); err != nil {
		t.Fatal(err)
	}
	assertShutdownBatchSummary(t, store, affected.ID, 1, 0, 1, 0, 0)
	terminal, err := store.GetBatch(t.Context(), affected.ID)
	if err != nil || terminal.Error != "original" || terminal.CompletedAt != "2000-01-01T00:00:00Z" {
		t.Fatalf("rewrote terminal parent: batch=%#v err=%v", terminal, err)
	}
	other, err := store.GetBatch(t.Context(), untouched.ID)
	if err != nil || other.Status != StatusCanceled || other.CanceledItems != 1 || other.Error != "original" || other.UpdatedAt != "2000-01-01T00:00:00Z" {
		t.Fatalf("rewrote unrelated batch: batch=%#v err=%v", other, err)
	}
}

func TestRecalculateBatchRetainsZeroTerminalSpeedAndETA(t *testing.T) {
	for _, status := range []string{StatusCompleted, StatusCanceled, StatusFailed} {
		t.Run(status, func(t *testing.T) {
			database, store, runtimeID := newAtomicTransferStore(t)
			batch := createAtomicBatch(t, store, runtimeID, 1)
			if _, err := database.Exec(`UPDATE file_transfer_batches SET status = ?, bytes_per_second = 25, eta_seconds = 3 WHERE id = ?`, status, batch.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := database.Exec(`UPDATE file_transfers SET status = ?, size_bytes = 100, transferred_bytes = 25, bytes_per_second = 25 WHERE batch_id = ?`, StatusRunning, batch.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.RecalculateBatch(t.Context(), batch.ID); err != nil {
				t.Fatal(err)
			}
			terminal, err := store.GetBatch(t.Context(), batch.ID)
			if err != nil || terminal.Status != status || terminal.BytesPerSecond != 0 || terminal.ETASeconds != 0 {
				t.Fatalf("invalid terminal statistics: batch=%#v err=%v", terminal, err)
			}
		})
	}
}

func TestFailActiveRecalculatesActiveParentWithOnlyTerminalChildren(t *testing.T) {
	database, store, runtimeID := newAtomicTransferStore(t)
	batch := createAtomicBatch(t, store, runtimeID, 2)
	if _, err := database.Exec(`UPDATE file_transfers SET status = ? WHERE id = ?`, StatusCompleted, batch.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE file_transfers SET status = ? WHERE id = ?`, StatusCanceled, batch.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE file_transfer_batches SET status = ?, bytes_per_second = 25, eta_seconds = 3 WHERE id = ?`, StatusRunning, batch.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.FailActive(t.Context(), "stopped", "stopped"); err != nil {
		t.Fatal(err)
	}
	assertShutdownBatchSummary(t, store, batch.ID, 2, 1, 0, 1, 0)
}

func assertShutdownBatchSummary(t *testing.T, store *Store, id int64, total, completed, failed, canceled int, transferred int64) {
	t.Helper()
	batch, err := store.GetBatch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != StatusFailed || batch.TotalItems != total || batch.CompletedItems != completed || batch.FailedItems != failed || batch.CanceledItems != canceled || batch.TransferredBytes != transferred || batch.BytesPerSecond != 0 || batch.ETASeconds != 0 {
		t.Fatalf("incorrect terminal summary: %#v", batch)
	}
}
