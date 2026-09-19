package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
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

func TestCheckpointFullRejectsIncompleteBusyCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint-busy.db")
	writer, err := OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	writer.SetMaxOpenConns(1)
	if _, err := writer.ExecContext(t.Context(), `PRAGMA journal_mode = WAL`); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(t.Context(), `PRAGMA busy_timeout = 1`); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenEncrypted(path, "test-password")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()

	transaction, err := reader.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	var count int
	if err := transaction.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM settings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.ExecContext(t.Context(), `INSERT INTO settings (key, value, updated_at) VALUES ('checkpoint-busy', 'value', datetime('now'))`); err != nil {
		t.Fatal(err)
	}

	if err := CheckpointFull(t.Context(), writer); err == nil || !strings.Contains(err.Error(), "remained busy") {
		t.Fatalf("CheckpointFull() error = %v, want busy checkpoint rejection", err)
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
