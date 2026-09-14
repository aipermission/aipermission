package transferruntime

import (
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
