package backupmigration

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/SE-I-T-Digital/go-sqlcipher"
)

func TestUploadExpiryStatementsPreserveRowsAndAddTerminalStatus(t *testing.T) {
	statements := UploadExpiryStatements()
	if len(statements) != 5 {
		t.Fatalf("statement count = %d, want 5", len(statements))
	}
	joined := strings.Join(statements, "\n")
	for _, required := range []string{"'expired'", "INSERT INTO backup_upload_operations_v35", "DROP TABLE backup_upload_operations", "idx_backup_upload_operations_provider_status"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("migration is missing %q", required)
		}
	}
}

func TestUploadExpiryStatementsExpireUnsafeLegacyOperations(t *testing.T) {
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	for _, statement := range []string{
		`CREATE TABLE backup_records (provider_id INTEGER NOT NULL, provider_file_id TEXT NOT NULL, deleted_at TEXT)`,
		`CREATE TABLE backup_upload_operations (
			idempotency_key TEXT PRIMARY KEY, provider_id INTEGER NOT NULL, database_id TEXT NOT NULL,
			stream_id TEXT NOT NULL, source_installation_id TEXT NOT NULL,
			status TEXT NOT NULL, provider_file_id TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL, updated_at TEXT NOT NULL, completed_at TEXT
		)`,
		`INSERT INTO backup_records(provider_id, provider_file_id, deleted_at) VALUES
			(1, 'deleted-backup', '2026-09-01T00:00:00Z'), (1, 'active-backup', NULL)`,
		`INSERT INTO backup_upload_operations(
			idempotency_key, provider_id, database_id, stream_id, source_installation_id,
			status, provider_file_id, last_error, created_at, updated_at, completed_at
		) VALUES
			('deleted-operation', 1, 'db', 'stream', 'install', 'completed', 'deleted-backup', '', 'created', 'updated', NULL),
			('active-operation', 1, 'db', 'stream', 'install', 'completed', 'active-backup', '', 'created', 'updated', 'completed'),
			('pending-operation', 1, 'db', 'stream', 'install', 'pending', '', '', 'created', 'updated', NULL),
			('dispatched-operation', 1, 'db', 'stream', 'install', 'dispatched', '', 'old dispatch error', 'created', 'updated', NULL),
			('unknown-operation', 1, 'db', 'stream', 'install', 'outcome_unknown', '', 'connection reset', 'created', 'updated', NULL)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range UploadExpiryStatements() {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}

	assertUploadOperation := func(key, wantStatus, wantError string, wantCompleted bool) {
		t.Helper()
		var status, lastError string
		var completedAt sql.NullString
		if err := database.QueryRow(`
			SELECT status, last_error, completed_at
			FROM backup_upload_operations WHERE idempotency_key = ?`, key).Scan(&status, &lastError, &completedAt); err != nil {
			t.Fatal(err)
		}
		if status != wantStatus || lastError != wantError || completedAt.Valid != wantCompleted {
			t.Fatalf("operation %q = status %q, error %q, completed %#v", key, status, lastError, completedAt)
		}
	}
	assertUploadOperation("deleted-operation", "expired", "remote upload result expired", true)
	assertUploadOperation("active-operation", "completed", "", true)
	assertUploadOperation("pending-operation", "expired", "upload outcome expired during protocol upgrade", true)
	assertUploadOperation("dispatched-operation", "expired", "upload outcome expired during protocol upgrade", true)
	assertUploadOperation("unknown-operation", "expired", "upload outcome expired during protocol upgrade", true)
}
