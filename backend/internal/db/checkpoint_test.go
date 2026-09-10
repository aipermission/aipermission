package db

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestCheckpointOperationsOwnSQLitePragmas(t *testing.T) {
	database, err := OpenEncrypted(filepath.Join(t.TempDir(), "checkpoint.db"), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(t.Context(), `INSERT INTO settings (key, value, updated_at) VALUES ('checkpoint-test', 'value', datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	if err := CheckpointFull(t.Context(), database); err != nil {
		t.Fatal(err)
	}
	if err := CheckpointForFilesystemMutation(t.Context(), database); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointOperationsRejectMissingDatabase(t *testing.T) {
	if err := CheckpointFull(t.Context(), nil); !errors.Is(err, ErrDatabaseNotOpen) {
		t.Fatalf("CheckpointFull() error = %v", err)
	}
	if err := CheckpointForFilesystemMutation(t.Context(), nil); !errors.Is(err, ErrDatabaseNotOpen) {
		t.Fatalf("CheckpointForFilesystemMutation() error = %v", err)
	}
}
