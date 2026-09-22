package snapshotfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
)

func TestCreateWritesConsistentSnapshotMetadata(t *testing.T) {
	sourceDir := t.TempDir()
	if err := os.Chmod(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(sourceDir, "source.aipdb")
	database, err := dbpkg.OpenEncrypted(sourcePath, "snapshot-test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	result, err := Create(t.Context(), Source{Database: database, DatabaseID: "test-workspace", Path: sourcePath}, 64<<20)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(result.Path) })
	if result.Path == sourcePath || !strings.HasSuffix(result.Filename, ".aipdb") || result.CreatedAt.IsZero() {
		t.Fatalf("unexpected snapshot result: %#v", result)
	}
	if info, err := os.Stat(result.Path); err != nil || info.Size() == 0 {
		t.Fatalf("snapshot file is unavailable: info=%v err=%v", info, err)
	}
}

func TestCreateRejectsMissingAndOversizedSources(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.aipdb")
	if _, err := Create(t.Context(), Source{Path: missing}, 1); err == nil || !strings.Contains(err.Error(), "inspect database") {
		t.Fatalf("missing source error = %v", err)
	}

	oversized := filepath.Join(t.TempDir(), "oversized.aipdb")
	if err := os.WriteFile(oversized, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(t.Context(), Source{Path: oversized}, 1); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized source error = %v", err)
	}
}
