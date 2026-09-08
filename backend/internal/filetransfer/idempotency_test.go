package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestIdempotentTransferAndBatchCreationReplaysAndRejectsDrift(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	transferRequest := CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionDownload, Source: SourceUI,
		RemotePath: "/tmp/result.txt", FileName: "result.txt", TempPath: "/tmp/download-result",
	}
	transferClaim := IdempotencyClaim{
		Scope: "ui", Key: "transfer-1", IdentityHash: "h1:transfer", ResourceKind: IdempotencyResourceTransfer,
	}
	firstTransfer, created, err := store.CreateIdempotent(ctx, transferRequest, transferClaim)
	if err != nil || !created {
		t.Fatalf("create idempotent transfer: created=%v err=%v", created, err)
	}
	replayedTransfer, created, err := store.CreateIdempotent(ctx, transferRequest, transferClaim)
	if err != nil || created || replayedTransfer.ID != firstTransfer.ID {
		t.Fatalf("replay idempotent transfer: item=%#v created=%v err=%v", replayedTransfer, created, err)
	}
	driftedTransferClaim := transferClaim
	driftedTransferClaim.IdentityHash = "h1:changed-transfer"
	if _, _, err := store.CreateIdempotent(ctx, transferRequest, driftedTransferClaim); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("drifted transfer claim error=%v", err)
	}

	batchRequest := CreateBatchRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		Items: []CreateRequest{{RemotePath: "/tmp/upload.txt", FileName: "upload.txt", TempPath: "/tmp/upload-result"}},
	}
	batchClaim := IdempotencyClaim{
		Scope: "ui", Key: "batch-1", IdentityHash: "h1:batch", ResourceKind: IdempotencyResourceBatch,
	}
	firstBatch, created, err := store.CreateBatchIdempotent(ctx, batchRequest, batchClaim)
	if err != nil || !created {
		t.Fatalf("create idempotent batch: created=%v err=%v", created, err)
	}
	replayedBatch, created, err := store.CreateBatchIdempotent(ctx, batchRequest, batchClaim)
	if err != nil || created || replayedBatch.ID != firstBatch.ID {
		t.Fatalf("replay idempotent batch: item=%#v created=%v err=%v", replayedBatch, created, err)
	}
	driftedBatchClaim := batchClaim
	driftedBatchClaim.IdentityHash = "h1:changed-batch"
	if _, _, err := store.CreateBatchIdempotent(ctx, batchRequest, driftedBatchClaim); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("drifted batch claim error=%v", err)
	}

	assertRowCount(t, database, "file_transfers", 2)
	assertRowCount(t, database, "file_transfer_batches", 1)
	assertRowCount(t, database, "file_transfer_start_idempotency", 2)
}

func TestConcurrentIdempotentBatchCreationClaimsOneBatch(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	request := CreateBatchRequest{
		RuntimeID: runtimeID, Direction: DirectionUpload, Source: SourceUI,
		Items: []CreateRequest{{RemotePath: "/tmp/concurrent.txt", FileName: "concurrent.txt"}},
	}
	claim := IdempotencyClaim{
		Scope: "ui", Key: "concurrent-batch", IdentityHash: "h1:concurrent", ResourceKind: IdempotencyResourceBatch,
	}

	const callers = 8
	results := make(chan BatchRecord, callers)
	errorsFound := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			batch, _, err := store.CreateBatchIdempotent(context.Background(), request, claim)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- batch
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent idempotent create: %v", err)
	}
	var batchID int64
	for result := range results {
		if batchID == 0 {
			batchID = result.ID
		}
		if result.ID != batchID {
			t.Fatalf("concurrent calls returned different batches: first=%d current=%d", batchID, result.ID)
		}
	}
	assertRowCount(t, database, "file_transfer_batches", 1)
	assertRowCount(t, database, "file_transfers", 1)
	if request.Items[0].RuntimeID != 0 || request.Items[0].QueueIndex != 0 || request.Items[0].Direction != "" || request.Items[0].Source != "" {
		t.Fatalf("batch normalization mutated caller-owned items: %#v", request.Items[0])
	}
}

func TestIdempotencyClaimRollsBackWithFailedTransferCreation(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	if _, err := database.Exec(`DROP TABLE audit_outbox`); err != nil {
		t.Fatalf("drop audit outbox: %v", err)
	}
	claim := IdempotencyClaim{
		Scope: "ui", Key: "rollback-1", IdentityHash: "h1:rollback", ResourceKind: IdempotencyResourceTransfer,
	}
	_, _, err = NewStore(database).CreateIdempotent(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionDownload, Source: SourceUI, RemotePath: "/tmp/result.txt",
	}, claim)
	if err == nil {
		t.Fatal("transfer creation should fail with its audit transaction")
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM file_transfer_start_idempotency`).Scan(&count); err != nil {
		t.Fatalf("count idempotency claims: %v", err)
	}
	if count != 0 {
		t.Fatalf("idempotency claim committed without transfer: count=%d", count)
	}
}

func TestIdempotentCreationRollsBackWhenHistoryProjectionFails(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	if _, err := database.Exec(`DROP TABLE history_entries`); err != nil {
		t.Fatalf("drop history entries: %v", err)
	}
	transferClaim := IdempotencyClaim{
		Scope: "ui", Key: "transfer-projection-failure", IdentityHash: "h1:transfer-projection", ResourceKind: IdempotencyResourceTransfer,
	}
	_, created, err := NewStore(database).CreateIdempotent(context.Background(), CreateRequest{
		RuntimeID:  runtimeID,
		Direction:  DirectionUpload,
		Source:     SourceUI,
		RemotePath: "/tmp/single-upload.txt",
		FileName:   "single-upload.txt",
		TempPath:   "/tmp/staged-single-upload",
	}, transferClaim)
	if err == nil || created {
		t.Fatalf("transfer creation should fail atomically with its history projection: created=%v err=%v", created, err)
	}
	assertRowCount(t, database, "file_transfers", 0)
	assertRowCount(t, database, "file_transfer_start_idempotency", 0)

	claim := IdempotencyClaim{
		Scope: "ui", Key: "batch-projection-failure", IdentityHash: "h1:batch-projection", ResourceKind: IdempotencyResourceBatch,
	}
	_, created, err = NewStore(database).CreateBatchIdempotent(context.Background(), CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: DirectionUpload,
		Source:    SourceUI,
		Items: []CreateRequest{{
			RemotePath: "/tmp/upload.txt",
			FileName:   "upload.txt",
			TempPath:   "/tmp/staged-upload",
		}},
	}, claim)
	if err == nil || created {
		t.Fatalf("batch creation should fail atomically with its history projection: created=%v err=%v", created, err)
	}
	assertRowCount(t, database, "file_transfers", 0)
	assertRowCount(t, database, "file_transfer_batches", 0)
	assertRowCount(t, database, "file_transfer_start_idempotency", 0)
}

func TestMarkBatchRunningRollsBackWhenHistoryProjectionFails(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	batch, err := store.CreateBatch(context.Background(), CreateBatchRequest{
		RuntimeID: runtimeID,
		Direction: DirectionUpload,
		Source:    SourceUI,
		Items: []CreateRequest{{
			RemotePath: "/tmp/upload.txt",
			FileName:   "upload.txt",
			TempPath:   "/tmp/staged-upload",
		}},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if _, err := database.Exec(`DROP TABLE history_entries`); err != nil {
		t.Fatalf("drop history entries: %v", err)
	}

	claimed, err := store.MarkBatchRunning(context.Background(), batch.ID)
	if err == nil || claimed {
		t.Fatalf("mark running should fail atomically with history projection: claimed=%v err=%v", claimed, err)
	}
	var status string
	if err := database.QueryRow(`SELECT status FROM file_transfer_batches WHERE id = ?`, batch.ID).Scan(&status); err != nil {
		t.Fatalf("read batch status: %v", err)
	}
	if status != StatusPending {
		t.Fatalf("batch status=%q want=%q", status, StatusPending)
	}
}

func TestIdempotencyTombstonePreventsReplayAfterTransferRetention(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeID := insertTestServer(t, database)
	store := NewStore(database)
	claim := IdempotencyClaim{
		Scope: "ui", Key: "retained-transfer", IdentityHash: "h1:retained", ResourceKind: IdempotencyResourceTransfer,
	}
	created, _, err := store.CreateIdempotent(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionDownload, Source: SourceUI, RemotePath: "/tmp/retained.txt",
	}, claim)
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM file_transfers WHERE id = ?`, created.ID); err != nil {
		t.Fatalf("delete retained transfer: %v", err)
	}
	if _, err := store.GetIdempotentTransfer(context.Background(), claim); !errors.Is(err, ErrIdempotencyResultExpired) {
		t.Fatalf("retained idempotency result error=%v", err)
	}
	if _, _, err := store.CreateIdempotent(context.Background(), CreateRequest{
		RuntimeID: runtimeID, Direction: DirectionDownload, Source: SourceUI, RemotePath: "/tmp/retained.txt",
	}, claim); !errors.Is(err, ErrIdempotencyResultExpired) {
		t.Fatalf("retained idempotency create error=%v", err)
	}
	assertRowCount(t, database, "file_transfer_start_idempotency", 1)
}

func assertRowCount(t *testing.T, database *sql.DB, table string, want int) {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s count=%d want=%d", table, count, want)
	}
}
