//go:build cgo

package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	sqlite3 "github.com/SE-I-T-Digital/go-sqlcipher"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestCanceledProgressRetainsWarmConnectionAndRollsBackHistory(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	created, err := store.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		LocalPath: "test.txt", RemotePath: "/tmp/test.txt", FileName: "test.txt", SizeBytes: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.MarkRunning(t.Context(), created.ID); err != nil || !ok {
		t.Fatalf("mark running: ok=%v err=%v", ok, err)
	}
	var before string
	if err := database.QueryRow(`SELECT preview_json FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, created.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	assertCanceledTransferRetainsConnection(t, database, func(ctx context.Context) error {
		return store.UpdateProgress(ctx, created.ID, 1024, 2048)
	})
	record, err := store.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var after string
	if err := database.QueryRow(`SELECT preview_json FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, created.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if record.TransferredBytes != 0 || before != after {
		t.Fatalf("canceled progress escaped rollback: bytes=%d before=%q after=%q", record.TransferredBytes, before, after)
	}
	if err := store.UpdateProgress(t.Context(), created.ID, 2048, 2048); err != nil {
		t.Fatal(err)
	}
	record, err = store.Get(t.Context(), created.ID)
	if err != nil || record.TransferredBytes != 2048 {
		t.Fatalf("subsequent progress was not committed: bytes=%d err=%v", record.TransferredBytes, err)
	}
}

func TestCanceledBatchRecalculationRetainsWarmConnectionAndRollsBackHistory(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	batch := createListBatch(t, store, runtimeID, "test.zip", DirectionUpload, StatusPending)
	id := batch.Items[0].ID
	if ok, err := store.MarkRunning(t.Context(), id); err != nil || !ok {
		t.Fatalf("mark running: ok=%v err=%v", ok, err)
	}
	if err := store.UpdateProgress(t.Context(), id, 5, 10); err != nil {
		t.Fatal(err)
	}
	assertCanceledTransferRetainsConnection(t, database, func(ctx context.Context) error {
		return store.RecalculateBatch(ctx, batch.ID)
	})
	var transferred int64
	if err := database.QueryRow("SELECT transferred_bytes FROM file_transfer_batches WHERE id = ?", batch.ID).Scan(&transferred); err != nil {
		t.Fatal(err)
	}
	if transferred != 0 {
		t.Fatalf("canceled batch update escaped rollback: bytes=%d", transferred)
	}
	if err := store.RecalculateBatch(t.Context(), batch.ID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow("SELECT transferred_bytes FROM file_transfer_batches WHERE id = ?", batch.ID).Scan(&transferred); err != nil {
		t.Fatal(err)
	}
	if transferred != 5 {
		t.Fatalf("subsequent batch update was not committed: bytes=%d", transferred)
	}
}

func assertCanceledTransferRetainsConnection(t *testing.T, database *sql.DB, write func(context.Context) error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var original *sqlite3.SQLiteConn
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error {
		original = raw.(*sqlite3.SQLiteConn)
		original.RegisterAuthorizer(func(operation int, table, _, _ string) int {
			if operation == sqlite3.SQLITE_UPDATE && table == "history_entries" {
				cancel()
				// Reject during prepare so this probe requires an acknowledged
				// rollback, not SQLite's timing-dependent interrupt auto-rollback.
				return sqlite3.SQLITE_DENY
			}
			return sqlite3.SQLITE_OK
		})
		return nil
	})
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	writeErr := write(ctx)
	var nativeErr sqlite3.Error
	if !errors.Is(ctx.Err(), context.Canceled) || !errors.As(writeErr, &nativeErr) || nativeErr.Code != sqlite3.ErrAuth || !transactionstate.IsNotCommitted(writeErr) {
		t.Fatalf("canceled rejected transfer must acknowledge rollback: ctx=%v err=%v", ctx.Err(), writeErr)
	}
	connection, err = database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error {
		next := raw.(*sqlite3.SQLiteConn)
		next.RegisterAuthorizer(nil)
		if next != original {
			t.Errorf("canceled transfer write discarded the warm encrypted connection: %v", writeErr)
		}
		if !next.AutoCommit() {
			t.Error("canceled transfer write left a transaction open")
		}
		return nil
	})
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestProgressNativeRollbackRetiresConnectionBeforeReadback(t *testing.T) {
	database, store, runtimeID := batchListFixture(t)
	created, err := store.Create(t.Context(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		LocalPath: "test.txt", RemotePath: "/tmp/test.txt", FileName: "test.txt", SizeBytes: 2048,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := store.MarkRunning(t.Context(), created.ID); err != nil || !ok {
		t.Fatalf("mark running: ok=%v err=%v", ok, err)
	}
	var before string
	if err := database.QueryRow(`SELECT preview_json FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, created.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	var original any
	connection, err := database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error { original = raw; return nil })
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fixture_native_rollback BEFORE UPDATE ON history_entries
		BEGIN SELECT RAISE(ROLLBACK, 'fixture native rollback'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.UpdateProgress(t.Context(), created.ID, 1024, 2048)
	if err == nil || transactionstate.IsNotCommitted(err) || !transactionstate.IsReadbackSafe(err) {
		t.Fatalf("unacknowledged rollback must retire before safe readback: %v", err)
	}
	connection, err = database.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	err = connection.Raw(func(raw any) error {
		if raw == original || !raw.(*sqlite3.SQLiteConn).AutoCommit() {
			t.Error("unacknowledged rollback retained the uncertain native connection")
		}
		return nil
	})
	_ = connection.Close()
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.Get(t.Context(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	var after string
	if err := database.QueryRow(`SELECT preview_json FROM history_entries WHERE source_ref_type = 'file_transfer' AND source_ref_id = ?`, created.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if record.TransferredBytes != 0 || before != after {
		t.Fatalf("native rollback did not preserve atomic evidence: bytes=%d", record.TransferredBytes)
	}
	if _, err := database.Exec("DROP TRIGGER fixture_native_rollback"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(t.Context(), created.ID, 2048, 2048); err != nil {
		t.Fatalf("replacement connection cannot persist progress: %v", err)
	}
	record, err = store.Get(t.Context(), created.ID)
	if err != nil || record.TransferredBytes != 2048 {
		t.Fatalf("replacement connection did not commit progress: bytes=%d err=%v", record.TransferredBytes, err)
	}
}
