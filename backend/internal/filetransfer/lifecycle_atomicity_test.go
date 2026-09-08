package filetransfer

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestTransferLifecycleRollsBackWithHistoryProjection(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(*testing.T, *Store, int64)
		mutate     func(*Store, int64) error
		wantBefore string
		wantAfter  string
	}{
		{name: "mark running", mutate: func(store *Store, id int64) error { _, err := store.MarkRunning(context.Background(), id); return err }, wantBefore: StatusPending, wantAfter: StatusRunning},
		{name: "complete", prepare: markAtomicTransferRunning, mutate: func(store *Store, id int64) error {
			_, err := store.Complete(context.Background(), id, 12, "checksum")
			return err
		}, wantBefore: StatusRunning, wantAfter: StatusCompleted},
		{name: "fail", prepare: markAtomicTransferRunning, mutate: func(store *Store, id int64) error {
			_, err := store.FailWithKind(context.Background(), id, "failed", FailureKindTimeout)
			return err
		}, wantBefore: StatusRunning, wantAfter: StatusFailed},
		{name: "cancel", mutate: func(store *Store, id int64) error {
			_, err := store.Cancel(context.Background(), id, "canceled")
			return err
		}, wantBefore: StatusPending, wantAfter: StatusCanceled},
		{name: "pause", prepare: markAtomicTransferRunning, mutate: func(store *Store, id int64) error { _, err := store.Pause(context.Background(), id); return err }, wantBefore: StatusRunning, wantAfter: StatusPaused},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, store, runtimeID := newAtomicTransferStore(t)
			item, err := store.Create(context.Background(), CreateRequest{
				RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
				RemotePath: "/fixture", FileName: "fixture", TempPath: "/tmp/fixture",
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.prepare != nil {
				test.prepare(t, store, item.ID)
			}
			installHistoryProjectionFailure(t, database, "INSERT")
			if err := test.mutate(store, item.ID); err == nil {
				t.Fatal("expected injected history projection failure")
			}
			assertAtomicTransferStatus(t, store, item.ID, test.wantBefore)
			dropHistoryProjectionFailure(t, database)
			if err := test.mutate(store, item.ID); err != nil {
				t.Fatalf("retry mutation: %v", err)
			}
			assertAtomicTransferStatus(t, store, item.ID, test.wantAfter)
		})
	}
}

func TestTransferProgressRollsBackWithHistoryProjection(t *testing.T) {
	database, store, runtimeID := newAtomicTransferStore(t)
	item, err := store.Create(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		RemotePath: "/fixture", FileName: "fixture", SizeBytes: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	markAtomicTransferRunning(t, store, item.ID)
	installHistoryProjectionFailure(t, database, "INSERT")
	if err := store.UpdateProgressStats(context.Background(), item.ID, 50, 100, 25, 2); err == nil {
		t.Fatal("expected injected history projection failure")
	}
	assertAtomicTransferProgress(t, database, item.ID, 0)
	dropHistoryProjectionFailure(t, database)
	if err := store.UpdateProgressStats(context.Background(), item.ID, 50, 100, 25, 2); err != nil {
		t.Fatalf("retry progress: %v", err)
	}
	assertAtomicTransferProgress(t, database, item.ID, 50)
}

func TestBatchLifecycleRollsBackWithHistoryProjection(t *testing.T) {
	t.Run("terminal transition", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 2)
		installHistoryProjectionFailure(t, database, "INSERT")
		if changed, err := store.CancelBatch(context.Background(), batch.ID, "cancel"); err == nil || changed {
			t.Fatalf("cancel batch changed=%v err=%v", changed, err)
		}
		assertAtomicBatchStatus(t, store, batch.ID, StatusPending, StatusPending)
		dropHistoryProjectionFailure(t, database)
		if changed, err := store.CancelBatch(context.Background(), batch.ID, "cancel"); err != nil || !changed {
			t.Fatalf("retry cancel batch changed=%v err=%v", changed, err)
		}
		assertAtomicBatchStatus(t, store, batch.ID, StatusCanceled, StatusCanceled)
	})

	t.Run("complete batch", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 1)
		if changed, err := store.MarkBatchRunning(context.Background(), batch.ID); err != nil || !changed {
			t.Fatalf("mark batch running: changed=%v err=%v", changed, err)
		}
		markAtomicTransferRunning(t, store, batch.Items[0].ID)
		if changed, err := store.Complete(context.Background(), batch.Items[0].ID, 12, "checksum"); err != nil || !changed {
			t.Fatalf("complete item: changed=%v err=%v", changed, err)
		}
		installHistoryProjectionFailure(t, database, "INSERT")
		if changed, err := store.CompleteBatch(context.Background(), batch.ID); err == nil || changed {
			t.Fatalf("complete batch changed=%v err=%v", changed, err)
		}
		assertAtomicBatchStatus(t, store, batch.ID, StatusRunning, StatusCompleted)
		dropHistoryProjectionFailure(t, database)
		if changed, err := store.CompleteBatch(context.Background(), batch.ID); err != nil || !changed {
			t.Fatalf("retry complete batch changed=%v err=%v", changed, err)
		}
		assertAtomicBatchStatus(t, store, batch.ID, StatusCompleted, StatusCompleted)
	})

	t.Run("fail active", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 1)
		installHistoryProjectionFailure(t, database, "INSERT")
		if err := store.FailActive(context.Background(), "transfer stopped", "batch stopped"); err == nil {
			t.Fatal("expected injected history projection failure")
		}
		assertAtomicBatchStatus(t, store, batch.ID, StatusPending, StatusPending)
		dropHistoryProjectionFailure(t, database)
		if err := store.FailActive(context.Background(), "transfer stopped", "batch stopped"); err != nil {
			t.Fatalf("retry fail active: %v", err)
		}
		assertAtomicBatchStatus(t, store, batch.ID, StatusFailed, StatusFailed)
	})

	t.Run("pending sizes", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 2)
		installHistoryProjectionFailure(t, database, "INSERT")
		sizes := map[int64]int64{batch.Items[0].ID: 10, batch.Items[1].ID: 20}
		if err := store.UpdatePendingBatchItemSizes(context.Background(), batch.ID, sizes); err == nil {
			t.Fatal("expected injected history projection failure")
		}
		assertAtomicBatchSize(t, store, batch.ID, 0)
		dropHistoryProjectionFailure(t, database)
		if err := store.UpdatePendingBatchItemSizes(context.Background(), batch.ID, sizes); err != nil {
			t.Fatalf("retry pending sizes: %v", err)
		}
		assertAtomicBatchSize(t, store, batch.ID, 30)
	})

	t.Run("recalculate", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 1)
		if _, err := database.Exec(`UPDATE file_transfers SET size_bytes = 42 WHERE id = ?`, batch.Items[0].ID); err != nil {
			t.Fatal(err)
		}
		installHistoryProjectionFailure(t, database, "INSERT")
		if err := store.RecalculateBatch(context.Background(), batch.ID); err == nil {
			t.Fatal("expected injected history projection failure")
		}
		assertAtomicBatchSize(t, store, batch.ID, 0)
		dropHistoryProjectionFailure(t, database)
		if err := store.RecalculateBatch(context.Background(), batch.ID); err != nil {
			t.Fatalf("retry recalculate: %v", err)
		}
		assertAtomicBatchSize(t, store, batch.ID, 42)
	})

	t.Run("archive", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 1)
		installHistoryProjectionFailure(t, database, "INSERT")
		if err := store.SetBatchArchive(context.Background(), batch.ID, "/tmp/archive.zip"); err == nil {
			t.Fatal("expected injected history projection failure")
		}
		assertAtomicBatchArchive(t, database, batch.ID, "")
		dropHistoryProjectionFailure(t, database)
		if err := store.SetBatchArchive(context.Background(), batch.ID, "/tmp/archive.zip"); err != nil {
			t.Fatalf("retry archive: %v", err)
		}
		assertAtomicBatchArchive(t, database, batch.ID, "/tmp/archive.zip")
	})

	t.Run("queue deletion", func(t *testing.T) {
		database, store, runtimeID := newAtomicTransferStore(t)
		batch := createAtomicBatch(t, store, runtimeID, 2)
		if changed, err := store.MarkBatchRunning(context.Background(), batch.ID); err != nil || !changed {
			t.Fatalf("mark batch running: %v", err)
		}
		markAtomicTransferRunning(t, store, batch.Items[0].ID)
		if changed, err := store.PauseBatch(context.Background(), batch.ID); err != nil || !changed {
			t.Fatalf("pause batch: %v", err)
		}
		installHistoryProjectionFailure(t, database, "DELETE")
		if _, err := store.UpdatePausedBatchQueue(context.Background(), batch.ID, nil); err == nil {
			t.Fatal("expected injected history deletion failure")
		}
		if _, err := store.Get(context.Background(), batch.Items[1].ID); err != nil {
			t.Fatalf("removed item was not rolled back: %v", err)
		}
		dropHistoryProjectionFailure(t, database)
		removed, err := store.UpdatePausedBatchQueue(context.Background(), batch.ID, nil)
		if err != nil || len(removed) != 1 {
			t.Fatalf("retry queue deletion removed=%d err=%v", len(removed), err)
		}
	})
}

func newAtomicTransferStore(t *testing.T) (*sql.DB, *Store, int64) {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "AtomicTransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, NewStore(database), insertTestServer(t, database)
}

func createAtomicBatch(t *testing.T, store *Store, runtimeID int64, count int) BatchRecord {
	t.Helper()
	items := make([]CreateRequest, 0, count)
	for index := 0; index < count; index++ {
		items = append(items, CreateRequest{
			RemotePath: fmt.Sprintf("/fixture-%d", index), FileName: fmt.Sprintf("fixture-%d", index),
			TempPath: fmt.Sprintf("/tmp/fixture-%d", index),
		})
	}
	batch, err := store.CreateBatch(context.Background(), CreateBatchRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI, Items: items,
	})
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func markAtomicTransferRunning(t *testing.T, store *Store, id int64) {
	t.Helper()
	if changed, err := store.MarkRunning(context.Background(), id); err != nil || !changed {
		t.Fatalf("mark transfer running: changed=%v err=%v", changed, err)
	}
}

func installHistoryProjectionFailure(t *testing.T, database *sql.DB, event string) {
	t.Helper()
	query := fmt.Sprintf(`CREATE TRIGGER reject_atomic_history_projection BEFORE %s ON history_entries
		BEGIN SELECT RAISE(ABORT, 'injected history projection failure'); END`, event)
	if _, err := database.Exec(query); err != nil {
		t.Fatal(err)
	}
}

func dropHistoryProjectionFailure(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`DROP TRIGGER reject_atomic_history_projection`); err != nil {
		t.Fatal(err)
	}
}

func assertAtomicTransferStatus(t *testing.T, store *Store, id int64, want string) {
	t.Helper()
	item, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != want {
		t.Fatalf("transfer status=%q want=%q", item.Status, want)
	}
}

func assertAtomicTransferProgress(t *testing.T, database *sql.DB, id int64, want int64) {
	t.Helper()
	var got int64
	if err := database.QueryRow(`SELECT transferred_bytes FROM file_transfers WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("transferred bytes=%d want=%d", got, want)
	}
}

func assertAtomicBatchStatus(t *testing.T, store *Store, id int64, batchStatus, itemStatus string) {
	t.Helper()
	batch, err := store.GetBatch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != batchStatus {
		t.Fatalf("batch status=%q want=%q", batch.Status, batchStatus)
	}
	for _, item := range batch.Items {
		if item.Status != itemStatus {
			t.Fatalf("item %d status=%q want=%q", item.ID, item.Status, itemStatus)
		}
	}
}

func assertAtomicBatchSize(t *testing.T, store *Store, id int64, want int64) {
	t.Helper()
	batch, err := store.GetBatch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if batch.SizeBytes != want {
		t.Fatalf("batch size=%d want=%d", batch.SizeBytes, want)
	}
}

func assertAtomicBatchArchive(t *testing.T, database *sql.DB, id int64, want string) {
	t.Helper()
	var got string
	if err := database.QueryRow(`SELECT archive_path FROM file_transfer_batches WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("batch archive=%q want=%q", got, want)
	}
}
