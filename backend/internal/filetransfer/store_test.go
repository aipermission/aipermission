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
