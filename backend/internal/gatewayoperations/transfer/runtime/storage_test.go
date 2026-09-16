package transferruntime

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

func TestTempScavengerIsScopedToOneWorkspace(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data", "aipermission.aipdb"), TempTTL: time.Hour})
	runtimeA := &Runtime{storageID: "workspace-a", store: filetransfer.NewStore(database)}
	runtimeB := &Runtime{storageID: "workspace-b", store: filetransfer.NewStore(database)}
	rootA, err := runner.EnsureTempRoot(runtimeA)
	if err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(rootA, "upload-old")
	if err := os.WriteFile(orphan, []byte("workspace A"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}

	if err := runner.RecoverTempCleanup(t.Context(), runtimeB); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(orphan); err != nil || string(content) != "workspace A" {
		t.Fatalf("workspace B scavenger changed workspace A temp: content=%q err=%v", content, err)
	}
	if err := runner.RecoverTempCleanup(t.Context(), runtimeA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("workspace A orphan was not removed: %v", err)
	}
}

func TestTempPathOperationsRejectAnotherWorkspaceNamespace(t *testing.T) {
	dataRoot := t.TempDir()
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(dataRoot, "data", "aipermission.aipdb"), TempTTL: time.Hour})
	runtimeA := &Runtime{storageID: "workspace-a", store: filetransfer.NewStore(nil)}
	runtimeB := &Runtime{storageID: "workspace-b", store: filetransfer.NewStore(nil)}
	rootA, err := runner.EnsureTempRoot(runtimeA)
	if err != nil {
		t.Fatal(err)
	}
	foreignPath := filepath.Join(rootA, "download-private")
	if err := os.WriteFile(foreignPath, []byte("workspace A"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !runner.TempPathAllowed(runtimeA, foreignPath) {
		t.Fatal("own workspace temp path was rejected")
	}
	if runner.TempPathAllowed(runtimeB, foreignPath) {
		t.Fatal("another workspace temp path was accepted")
	}
	runner.RemoveTempPath(runtimeB, foreignPath)
	if content, err := os.ReadFile(foreignPath); err != nil || string(content) != "workspace A" {
		t.Fatalf("another workspace removed temp content: content=%q err=%v", content, err)
	}
}

func TestTempRootRejectsSymlinkedWorkspaceNamespace(t *testing.T) {
	dataRoot := t.TempDir()
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(dataRoot, "data", "aipermission.aipdb"), TempTTL: time.Hour})
	runtime := &Runtime{storageID: "workspace", store: filetransfer.NewStore(nil)}
	workspaceRoot, err := runner.workspaceTempRoot(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(workspaceRoot), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, workspaceRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.EnsureTempRoot(runtime); err == nil || !strings.Contains(err.Error(), "symbolic links") {
		t.Fatalf("symlinked workspace temp root = %v", err)
	}
}

func TestTempRecoveryDetachesForeignWorkspacePathsWithoutDeletingThem(t *testing.T) {
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "transfer.aipdb"), "TransferPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := filetransfer.NewStore(database)
	runtimeID := insertTransferRuntime(t, database)
	runner := NewRunner(RunnerConfig{DataPath: filepath.Join(t.TempDir(), "data", "aipermission.aipdb"), TempTTL: time.Hour})
	foreignRuntime := &Runtime{storageID: "workspace-a", store: store}
	runtime := &Runtime{storageID: "workspace-b", store: store}
	foreignRoot, err := runner.EnsureTempRoot(foreignRuntime)
	if err != nil {
		t.Fatal(err)
	}
	transferPath := filepath.Join(foreignRoot, "download-complete")
	archivePath := filepath.Join(foreignRoot, "archive-complete.zip")
	for _, path := range []string{transferPath, archivePath} {
		if err := os.WriteFile(path, []byte("preserve"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	transfer, err := store.Create(t.Context(), filetransfer.CreateRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		RemotePath: "/complete", FileName: "complete", TempPath: transferPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := store.MarkRunning(t.Context(), transfer.ID); err != nil || !changed {
		t.Fatalf("mark transfer running: changed=%v err=%v", changed, err)
	}
	if changed, err := store.Complete(t.Context(), transfer.ID, 8, ""); err != nil || !changed {
		t.Fatalf("complete transfer: changed=%v err=%v", changed, err)
	}
	batch, err := store.CreateBatch(t.Context(), filetransfer.CreateBatchRequest{
		RuntimeID: runtimeID, Direction: filetransfer.DirectionDownload, Source: filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{{RemotePath: "/batch", FileName: "batch"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetBatchArchive(t.Context(), batch.ID, archivePath, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := runner.RecoverTempCleanup(t.Context(), runtime); err != nil {
		t.Fatal(err)
	}
	storedTransfer, err := store.Get(t.Context(), transfer.ID)
	if err != nil || storedTransfer.TempPath != "" {
		t.Fatalf("unsafe transfer reference was not detached: path=%q err=%v", storedTransfer.TempPath, err)
	}
	storedBatch, err := store.GetBatch(t.Context(), batch.ID)
	if err != nil || storedBatch.ArchivePath != "" {
		t.Fatalf("unsafe archive reference was not detached: path=%q err=%v", storedBatch.ArchivePath, err)
	}
	for _, path := range []string{transferPath, archivePath} {
		if content, err := os.ReadFile(path); err != nil || string(content) != "preserve" {
			t.Fatalf("foreign workspace content changed: path=%q content=%q err=%v", path, content, err)
		}
	}
}

func insertTransferRuntime(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	target, err := database.Exec(`INSERT INTO connector_targets
		(project_id, connector_kind, name, config_json, created_at, updated_at)
		VALUES ((SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active'), 'ssh', 'fixture', '{}', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatal(err)
	}
	targetID, _ := target.LastInsertId()
	profile, err := database.Exec(`INSERT INTO connector_credential_profiles
		(target_id, connector_kind, kind, label, encrypted_secret_json, created_at, updated_at)
		VALUES (?, 'ssh', 'private_key', 'fixture', 'encrypted', datetime('now'), datetime('now'))`, targetID)
	if err != nil {
		t.Fatal(err)
	}
	profileID, _ := profile.LastInsertId()
	surface, err := database.Exec(`INSERT INTO connector_runtime_surfaces
		(connector_kind, target_id, profile_id, capability_kind, label, created_at, updated_at)
		VALUES ('ssh', ?, ?, 'live_console', 'fixture', datetime('now'), datetime('now'))`, targetID, profileID)
	if err != nil {
		t.Fatal(err)
	}
	runtimeID, _ := surface.LastInsertId()
	return runtimeID
}
