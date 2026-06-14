package filetransfer

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestStoreCreatesListsAndUpdatesFileTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeProfileID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	created, err := store.Create(ctx, CreateRequest{
		RuntimeProfileID: runtimeProfileID,
		Direction:        DirectionUpload,
		Source:           SourceUI,
		LocalPath:        "deploy.tar.gz",
		RemotePath:       "/tmp/deploy.tar.gz",
		FileName:         "deploy.tar.gz",
		SizeBytes:        2048,
		TempPath:         "/tmp/aipermission/staged",
	})
	if err != nil {
		t.Fatalf("create file transfer: %v", err)
	}
	if created.Status != StatusPending || created.Direction != DirectionUpload || created.TargetName != "worker-1" {
		t.Fatalf("unexpected created transfer: %#v", created)
	}
	if created.TempPath != "/tmp/aipermission/staged" {
		t.Fatalf("store should keep internal temp path for backend cleanup")
	}

	if ok, err := store.MarkRunning(ctx, created.ID); err != nil || !ok {
		t.Fatalf("mark running: %v", err)
	}
	if err := store.UpdateProgress(ctx, created.ID, 1024, 2048); err != nil {
		t.Fatalf("update progress: %v", err)
	}
	if ok, err := store.Complete(ctx, created.ID, 2048, "abc123"); err != nil || !ok {
		t.Fatalf("complete transfer: %v", err)
	}
	completed, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get completed transfer: %v", err)
	}
	if completed.Status != StatusCompleted || completed.TransferredBytes != 2048 || completed.ChecksumSHA256 != "abc123" || completed.CompletedAt == "" {
		t.Fatalf("unexpected completed transfer: %#v", completed)
	}

	items, total, err := store.List(ctx, ListFilter{Direction: DirectionUpload, Query: "deploy"})
	if err != nil {
		t.Fatalf("list transfers: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("unexpected transfer list: total=%d items=%#v", total, items)
	}

	if ok, err := store.Cancel(ctx, created.ID, "too late"); err != nil || ok {
		t.Fatalf("completed transfer should not be cancelable: ok=%v err=%v", ok, err)
	}
}

func TestStoreCreatesPausesAndCompletesBatches(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeProfileID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeProfileID: runtimeProfileID,
		Direction:        DirectionUpload,
		Source:           SourceUI,
		Items: []CreateRequest{
			{LocalPath: "a.txt", RemotePath: "/tmp/a.txt", FileName: "a.txt", SizeBytes: 100, TempPath: "/tmp/a"},
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", SizeBytes: 200, TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if batch.Status != StatusPending || batch.TotalItems != 2 || batch.SizeBytes != 300 || len(batch.Items) != 2 {
		t.Fatalf("unexpected batch: %#v", batch)
	}
	if batch.Items[0].BatchID != batch.ID || batch.Items[0].QueueIndex != 0 || batch.Items[1].QueueIndex != 1 {
		t.Fatalf("unexpected batch item ordering: %#v", batch.Items)
	}

	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark item running: ok=%v err=%v", ok, err)
	}
	if err := store.UpdateProgressStats(ctx, batch.Items[0].ID, 50, 100, 25, 2); err != nil {
		t.Fatalf("update item progress: %v", err)
	}
	if err := store.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate batch: %v", err)
	}
	progress, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch progress: %v", err)
	}
	if progress.TransferredBytes != 50 || progress.BytesPerSecond != 25 || progress.ETASeconds < 0 {
		t.Fatalf("unexpected batch progress: %#v", progress)
	}

	if ok, err := store.PauseBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("pause batch: ok=%v err=%v", ok, err)
	}
	paused, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get paused batch: %v", err)
	}
	if paused.Status != StatusPaused || paused.Items[0].Status != StatusPaused {
		t.Fatalf("unexpected paused batch: %#v", paused)
	}
	if ok, err := store.ResumeBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("resume batch: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[0].ID, 100, "aaa"); err != nil || !ok {
		t.Fatalf("complete first item: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[1].ID); err != nil || !ok {
		t.Fatalf("mark second item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[1].ID, 200, "bbb"); err != nil || !ok {
		t.Fatalf("complete second item: ok=%v err=%v", ok, err)
	}
	if err := store.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate completed batch: %v", err)
	}
	if ok, err := store.CompleteBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("complete batch: ok=%v err=%v", ok, err)
	}
	completed, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get completed batch: %v", err)
	}
	if completed.Status != StatusCompleted || completed.CompletedItems != 2 || completed.TransferredBytes != 300 || completed.ETASeconds != 0 {
		t.Fatalf("unexpected completed batch: %#v", completed)
	}

	batches, total, err := store.ListBatches(ctx, BatchListFilter{Direction: DirectionUpload, TargetIDs: []int64{runtimeProfileID}, Query: "worker"})
	if err != nil {
		t.Fatalf("list batches: %v", err)
	}
	if total != 1 || len(batches) != 1 || batches[0].ID != batch.ID {
		t.Fatalf("unexpected batch list: total=%d items=%#v", total, batches)
	}
	batches, total, err = store.ListBatches(ctx, BatchListFilter{TargetIDs: []int64{runtimeProfileID + 1000}})
	if err != nil {
		t.Fatalf("list filtered batches: %v", err)
	}
	if total != 0 || len(batches) != 0 {
		t.Fatalf("unexpected filtered batch list: total=%d items=%#v", total, batches)
	}
}

func TestStoreApprovesPendingTransferBatchItems(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeProfileID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeProfileID: runtimeProfileID,
		Direction:        DirectionDownload,
		Source:           SourceMCP,
		Status:           StatusPendingApproval,
		ApprovalNote:     "initial",
		Items: []CreateRequest{
			{RemotePath: "/tmp/a.log", FileName: "a.log", SizeBytes: 100, TempPath: "/tmp/a"},
			{RemotePath: "/tmp/b.log", FileName: "b.log", SizeBytes: 200, TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create pending approval batch: %v", err)
	}
	if batch.Status != StatusPendingApproval || batch.Items[0].Status != StatusPendingApproval {
		t.Fatalf("unexpected pending approval batch: %#v", batch)
	}

	approved, rejected, err := store.ApproveBatch(ctx, batch.ID, BatchApprovalRequest{
		ApprovedItemIDs: []int64{batch.Items[0].ID},
		Note:            "skip b.log",
	})
	if err != nil {
		t.Fatalf("approve batch: %v", err)
	}
	if approved.Status != StatusPending || approved.ApprovalNote != "skip b.log" || len(rejected) != 1 || rejected[0].ID != batch.Items[1].ID {
		t.Fatalf("unexpected approved batch: batch=%#v rejected=%#v", approved, rejected)
	}
	if approved.Items[0].Status != StatusPending || approved.Items[1].Status != StatusCanceled || approved.Items[1].Error != "skip b.log" {
		t.Fatalf("unexpected approved items: %#v", approved.Items)
	}

	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark approved batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark approved item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.Complete(ctx, batch.Items[0].ID, 100, "checksum"); err != nil || !ok {
		t.Fatalf("complete approved item: ok=%v err=%v", ok, err)
	}
	if err := store.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate partial batch: %v", err)
	}
	if ok, err := store.CompleteBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("complete partial batch: ok=%v err=%v", ok, err)
	}
	completed, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get completed partial batch: %v", err)
	}
	if completed.Status != StatusCompleted || completed.CompletedItems != 1 || completed.CanceledItems != 1 {
		t.Fatalf("unexpected completed partial batch: %#v", completed)
	}
}

func TestStoreUpdatesPausedBatchQueue(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeProfileID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeProfileID: runtimeProfileID,
		Direction:        DirectionUpload,
		Source:           SourceUI,
		Items: []CreateRequest{
			{LocalPath: "a.txt", RemotePath: "/tmp/a.txt", FileName: "a.txt", SizeBytes: 100, TempPath: "/tmp/a"},
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", SizeBytes: 200, TempPath: "/tmp/b"},
			{LocalPath: "c.txt", RemotePath: "/tmp/c.txt", FileName: "c.txt", SizeBytes: 300, TempPath: "/tmp/c"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark first item running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.PauseBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("pause batch: ok=%v err=%v", ok, err)
	}

	removed, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[2].ID})
	if err != nil {
		t.Fatalf("update paused queue: %v", err)
	}
	if len(removed) != 1 || removed[0].ID != batch.Items[1].ID || removed[0].TempPath != "/tmp/b" {
		t.Fatalf("unexpected removed items: %#v", removed)
	}
	if _, err := store.Get(ctx, batch.Items[1].ID); err != ErrNotFound {
		t.Fatalf("removed pending item should be deleted, got err=%v", err)
	}
	updated, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get updated batch: %v", err)
	}
	if updated.TotalItems != 2 || updated.SizeBytes != 400 {
		t.Fatalf("unexpected recalculated batch: %#v", updated)
	}
	if updated.Items[0].ID != batch.Items[0].ID || updated.Items[0].Status != StatusPaused {
		t.Fatalf("paused running item should stay in place: %#v", updated.Items)
	}
	if updated.Items[1].ID != batch.Items[2].ID || updated.Items[1].Status != StatusPending {
		t.Fatalf("remaining pending item should stay queued: %#v", updated.Items)
	}
	if _, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[0].ID}); err != ErrInvalidState {
		t.Fatalf("non-pending items must not be editable, got err=%v", err)
	}
	if _, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{0}); err != ErrInvalidArgument {
		t.Fatalf("non-positive item ids should fail as invalid arguments, got err=%v", err)
	}
	if _, err := store.UpdatePausedBatchQueue(ctx, batch.ID, []int64{batch.Items[2].ID, batch.Items[2].ID}); err != ErrInvalidArgument {
		t.Fatalf("duplicate item ids should fail as invalid arguments, got err=%v", err)
	}
}

func TestStoreFailsActiveTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	runtimeProfileID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	standalone, err := store.Create(ctx, CreateRequest{
		RuntimeProfileID: runtimeProfileID,
		Direction:        DirectionDownload,
		Source:           SourceUI,
		RemotePath:       "/tmp/a.txt",
		FileName:         "a.txt",
		TempPath:         "/tmp/a",
	})
	if err != nil {
		t.Fatalf("create standalone transfer: %v", err)
	}
	if ok, err := store.MarkRunning(ctx, standalone.ID); err != nil || !ok {
		t.Fatalf("mark standalone running: ok=%v err=%v", ok, err)
	}
	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		RuntimeProfileID: runtimeProfileID,
		Direction:        DirectionUpload,
		Source:           SourceUI,
		Items: []CreateRequest{
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", SizeBytes: 200, TempPath: "/tmp/b"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := store.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := store.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark batch item running: ok=%v err=%v", ok, err)
	}

	if err := store.FailActive(ctx, "transfer stopped", "batch stopped"); err != nil {
		t.Fatalf("fail active transfers: %v", err)
	}
	updated, err := store.Get(ctx, standalone.ID)
	if err != nil {
		t.Fatalf("get standalone: %v", err)
	}
	if updated.Status != StatusFailed || updated.Error != "transfer stopped" || updated.CompletedAt == "" {
		t.Fatalf("unexpected failed standalone transfer: %#v", updated)
	}
	updatedBatch, err := store.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	if updatedBatch.Status != StatusFailed || updatedBatch.Error != "batch stopped" || updatedBatch.Items[0].Status != StatusFailed {
		t.Fatalf("unexpected failed batch: %#v", updatedBatch)
	}
}

func TestStoreValidatesFileTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := NewStore(database)

	if _, err := store.Create(context.Background(), CreateRequest{Direction: DirectionUpload, RemotePath: "/tmp/file"}); err == nil {
		t.Fatalf("missing runtime_profile_id should fail")
	}
	if _, err := store.Create(context.Background(), CreateRequest{RuntimeProfileID: 1, Direction: "copy", RemotePath: "/tmp/file"}); err == nil {
		t.Fatalf("invalid direction should fail")
	}
	if _, err := store.Create(context.Background(), CreateRequest{RuntimeProfileID: 1, Direction: DirectionDownload, RemotePath: "bad\npath"}); err == nil {
		t.Fatalf("control characters should fail")
	}
}

func insertTestServer(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	targetResult, err := database.Exec(
		`INSERT INTO connector_targets (connector_kind, name, config_json, created_at, updated_at)
		VALUES ('ssh', ?, ?, datetime('now'), datetime('now'))`,
		"worker-1",
		`{"host":"127.0.0.1","port":22}`,
	)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := targetResult.LastInsertId()
	if err != nil {
		t.Fatalf("target id: %v", err)
	}
	profileResult, err := database.Exec(
		`INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at
		)
		VALUES (?, 'ssh', 'private_key', 'root', '{"username":"root","ssh_key_id":1}', 'encrypted', datetime('now'), datetime('now'))`,
		targetID,
	)
	if err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	profileID, err := profileResult.LastInsertId()
	if err != nil {
		t.Fatalf("profile id: %v", err)
	}
	return profileID
}
