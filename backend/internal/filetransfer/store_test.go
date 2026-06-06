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
	serverID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	created, err := store.Create(ctx, CreateRequest{
		ServerID:   serverID,
		Direction:  DirectionUpload,
		Source:     SourceUI,
		LocalPath:  "deploy.tar.gz",
		RemotePath: "/tmp/deploy.tar.gz",
		FileName:   "deploy.tar.gz",
		SizeBytes:  2048,
		TempPath:   "/tmp/aipermission/staged",
	})
	if err != nil {
		t.Fatalf("create file transfer: %v", err)
	}
	if created.Status != StatusPending || created.Direction != DirectionUpload || created.ServerName != "worker-1" {
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
	serverID := insertTestServer(t, database)
	store := NewStore(database)
	ctx := context.Background()

	batch, err := store.CreateBatch(ctx, CreateBatchRequest{
		ServerID:  serverID,
		Direction: DirectionUpload,
		Source:    SourceUI,
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
}

func TestStoreValidatesFileTransfers(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "secure.db"), "TransferPassword123")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	store := NewStore(database)

	if _, err := store.Create(context.Background(), CreateRequest{Direction: DirectionUpload, RemotePath: "/tmp/file"}); err == nil {
		t.Fatalf("missing server_id should fail")
	}
	if _, err := store.Create(context.Background(), CreateRequest{ServerID: 1, Direction: "copy", RemotePath: "/tmp/file"}); err == nil {
		t.Fatalf("invalid direction should fail")
	}
	if _, err := store.Create(context.Background(), CreateRequest{ServerID: 1, Direction: DirectionDownload, RemotePath: "bad\npath"}); err == nil {
		t.Fatalf("control characters should fail")
	}
}

func insertTestServer(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	result, err := database.Exec(
		`INSERT INTO servers (name, host, port, username, ssh_key_id, auth_type, key_label, encrypted_secret, description, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		"worker-1",
		"127.0.0.1",
		22,
		"root",
		1,
		"private_key",
		"",
		"gateway-managed-ssh-key",
		"",
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("server id: %v", err)
	}
	return id
}
