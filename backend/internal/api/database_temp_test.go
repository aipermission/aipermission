package api

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestScavengeDatabaseTempPathsRemovesOnlyStaleTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	defaultPath := filepath.Join(root, "aipermission.db")
	directory := filepath.Join(root, databaseTempDirectoryName)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(directory, "snapshot-stale.aipdb")
	staleRemote := filepath.Join(directory, "remote-backup-1-stale.aipdb")
	staleFirstRun := filepath.Join(directory, "first-run-restore-stale.aipdb")
	legacyRemote := filepath.Join(root, ".remote-backup-legacy.aipdb")
	legacyFirstRun := filepath.Join(root, "databases", ".first-run-restore-legacy.aipdb")
	legacyBackup := filepath.Join(root, ".default-123.backup.aipdb")
	fresh := filepath.Join(directory, "import-fresh.aipdb")
	unowned := filepath.Join(directory, "operator-note.txt")
	if err := os.MkdirAll(filepath.Dir(legacyFirstRun), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{stale, staleRemote, staleFirstRun, legacyRemote, legacyFirstRun, legacyBackup, fresh, unowned} {
		if err := os.WriteFile(path, []byte("temporary"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	if err := os.Chtimes(stale, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{staleRemote, staleFirstRun, legacyRemote, legacyFirstRun, legacyBackup} {
		if err := os.Chtimes(path, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(unowned, now.Add(-25*time.Hour), now.Add(-25*time.Hour)); err != nil {
		t.Fatal(err)
	}
	scavengeDatabaseTempPaths(defaultPath, now)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale temporary file remains: %v", err)
	}
	for _, path := range []string{staleRemote, staleFirstRun, legacyRemote, legacyFirstRun, legacyBackup} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stale remote temporary file remains: %s: %v", path, err)
		}
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh temporary file was removed: %v", err)
	}
	if _, err := os.Stat(unowned); err != nil {
		t.Fatalf("unowned file was removed: %v", err)
	}
}
