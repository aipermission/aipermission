package databasecatalog

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/db"
)

func TestMoveFileNoReplacePreservesExistingTarget(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.aipdb")
	targetPath := filepath.Join(directory, "target.aipdb")
	if err := os.WriteFile(sourcePath, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("foreign"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(sourcePath, targetPath); !errors.Is(err, db.ErrPublishTargetExists) {
		t.Fatalf("move conflict = %v", err)
	}
	if content, err := os.ReadFile(targetPath); err != nil || string(content) != "foreign" {
		t.Fatalf("foreign target changed: content=%q err=%v", content, err)
	}
	if content, err := os.ReadFile(sourcePath); err != nil || string(content) != "candidate" {
		t.Fatalf("move source changed: content=%q err=%v", content, err)
	}
}

func TestMoveFileNoReplaceUsesNativeMove(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "source.aipdb")
	targetPath := filepath.Join(directory, "target.aipdb")
	if err := os.WriteFile(sourcePath, []byte("candidate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFileNoReplace(sourcePath, targetPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("move source still exists: %v", err)
	}
	if content, err := os.ReadFile(targetPath); err != nil || string(content) != "candidate" {
		t.Fatalf("moved target = %q err=%v", content, err)
	}
}
