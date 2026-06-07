package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenEncryptedCreatesSchemaAndRejectsWrongPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	defer database.Close()

	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'api_tokens'`).Scan(&count); err != nil {
		t.Fatalf("query schema: %v", err)
	}
	if count != 1 {
		t.Fatalf("api_tokens table was not created")
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, currentSchemaVersion).Scan(&count); err != nil {
		t.Fatalf("query schema migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema migration version was not recorded")
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query migration count: %v", err)
	}
	if count != currentSchemaVersion {
		t.Fatalf("expected %d recorded migrations, got %d", currentSchemaVersion, count)
	}
	if !tableExists(t, database, "redaction_rules") {
		t.Fatalf("redaction_rules table was not created")
	}
	if !tableExists(t, database, "console_session_chunks") {
		t.Fatalf("console_session_chunks table was not created")
	}
	if !tableExists(t, database, "history_labels") {
		t.Fatalf("history_labels table was not created")
	}
	if !tableExists(t, database, "command_request_labels") {
		t.Fatalf("command_request_labels table was not created")
	}
	if !tableExists(t, database, "file_transfers") {
		t.Fatalf("file_transfers table was not created")
	}
	if !tableExists(t, database, "file_transfer_batches") {
		t.Fatalf("file_transfer_batches table was not created")
	}
	if !columnExists(t, database, "api_tokens", "expires_at") {
		t.Fatalf("api_tokens.expires_at column was not created")
	}
	if !columnExists(t, database, "token_server_permissions", "expires_at") {
		t.Fatalf("token_server_permissions.expires_at column was not created")
	}
	if !columnExists(t, database, "command_requests", "source") {
		t.Fatalf("command_requests.source column was not created")
	}
	if !columnExists(t, database, "command_requests", "tracking_reason") {
		t.Fatalf("command_requests.tracking_reason column was not created")
	}
	if !columnExists(t, database, "command_requests", "output_truncated") {
		t.Fatalf("command_requests.output_truncated column was not created")
	}
	if !columnExists(t, database, "command_requests", "approval_context") {
		t.Fatalf("command_requests.approval_context column was not created")
	}
	if !columnExists(t, database, "command_requests", "approval_context_hash") {
		t.Fatalf("command_requests.approval_context_hash column was not created")
	}
	if !columnExists(t, database, "command_requests", "approval_context_drift") {
		t.Fatalf("command_requests.approval_context_drift column was not created")
	}
	if !columnExists(t, database, "file_transfer_batches", "approval_note") {
		t.Fatalf("file_transfer_batches.approval_note column was not created")
	}
	if !columnExists(t, database, "file_transfer_batches", "overwrite") {
		t.Fatalf("file_transfer_batches.overwrite column was not created")
	}
	var foreignKeys int
	if err := database.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign keys should be enabled for the connection")
	}
	if _, err := database.Exec(`
		INSERT INTO token_server_permissions (token_id, server_id, execution_rule, created_at, updated_at)
		VALUES (999, 999, 'always_run', datetime('now'), datetime('now'))`,
	); err == nil {
		t.Fatalf("foreign key violation should fail")
	}
	if LooksLikePlainSQLite(path) {
		t.Fatalf("encrypted database should not have plaintext sqlite header")
	}

	if wrong, err := OpenEncrypted(path, "wrong-password"); err == nil {
		_ = wrong.Close()
		t.Fatalf("expected wrong password to fail")
	}
}

func TestOpenEncryptedRepairsMissingHistoryLabelSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	if _, err := database.Exec(`DROP TABLE command_request_labels`); err != nil {
		t.Fatalf("drop command_request_labels: %v", err)
	}
	if _, err := database.Exec(`DROP TABLE history_labels`); err != nil {
		t.Fatalf("drop history_labels: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("reopen encrypted db: %v", err)
	}
	defer reopened.Close()
	if !tableExists(t, reopened, "history_labels") {
		t.Fatalf("history_labels table should be repaired")
	}
	if !tableExists(t, reopened, "command_request_labels") {
		t.Fatalf("command_request_labels table should be repaired")
	}
}

func TestOpenEncryptedRepairsMissingFileTransferSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	if _, err := database.Exec(`DROP TABLE file_transfers`); err != nil {
		t.Fatalf("drop file_transfers: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("reopen encrypted db: %v", err)
	}
	defer reopened.Close()
	if !tableExists(t, reopened, "file_transfers") {
		t.Fatalf("file_transfers table should be repaired")
	}
}

func TestOpenEncryptedAppliesSQLCipherPragmas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "PragmaPassword123")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	defer database.Close()

	var cipherVersion string
	if err := database.QueryRow(`PRAGMA cipher_version`).Scan(&cipherVersion); err != nil {
		t.Fatalf("query SQLCipher version: %v", err)
	}
	if cipherVersion == "" {
		t.Fatalf("SQLCipher version should not be empty")
	}

	var cipherPageSize int
	if err := database.QueryRow(`PRAGMA cipher_page_size`).Scan(&cipherPageSize); err != nil {
		t.Fatalf("query SQLCipher page size: %v", err)
	}
	if cipherPageSize != 4096 {
		t.Fatalf("expected SQLCipher page size 4096, got %d", cipherPageSize)
	}

	var kdfIterations int
	if err := database.QueryRow(`PRAGMA kdf_iter`).Scan(&kdfIterations); err != nil {
		t.Fatalf("query SQLCipher KDF iterations: %v", err)
	}
	if kdfIterations <= 0 {
		t.Fatalf("SQLCipher KDF iterations should be positive, got %d", kdfIterations)
	}
}

func TestRekeyChangesEncryptedDatabasePassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "old-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('gateway_secret', 'secret', datetime('now'))`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if err := ValidateEncrypted(path, "old-password"); err != nil {
		t.Fatalf("validate current password: %v", err)
	}
	if err := ValidateEncrypted(path, "wrong-password"); err == nil {
		t.Fatalf("expected wrong password validation to fail")
	}
	if err := Rekey(database, "new-password"); err != nil {
		t.Fatalf("rekey encrypted db: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close rekeyed db: %v", err)
	}

	if old, err := OpenEncrypted(path, "old-password"); err == nil {
		_ = old.Close()
		t.Fatalf("expected old password to fail after rekey")
	}
	if err := ValidateEncrypted(path, "old-password"); err == nil {
		t.Fatalf("expected old password validation to fail after rekey")
	}
	if err := ValidateEncrypted(path, "new-password"); err != nil {
		t.Fatalf("validate new password after rekey: %v", err)
	}
	reopened, err := OpenEncrypted(path, "new-password")
	if err != nil {
		t.Fatalf("open with new password: %v", err)
	}
	defer reopened.Close()
	var value string
	if err := reopened.QueryRow(`SELECT value FROM settings WHERE key = 'gateway_secret'`).Scan(&value); err != nil {
		t.Fatalf("read setting after rekey: %v", err)
	}
	if value != "secret" {
		t.Fatalf("unexpected setting after rekey: %s", value)
	}
}

func TestEncryptedDatabasePasswordsAllowSQLSpecialCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	password := `Strong'Password";--123`
	nextPassword := `Next'Password"; VACUUM; 456Aa`
	database, err := OpenEncrypted(path, password)
	if err != nil {
		t.Fatalf("open encrypted db with special characters: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('special_password_test', 'ok', datetime('now'))`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if err := Rekey(database, nextPassword); err != nil {
		t.Fatalf("rekey with special characters: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close rekeyed db: %v", err)
	}
	if err := ValidateEncrypted(path, password); err == nil {
		t.Fatalf("old special-character password should fail after rekey")
	}
	reopened, err := OpenEncrypted(path, nextPassword)
	if err != nil {
		t.Fatalf("open with new special-character password: %v", err)
	}
	defer reopened.Close()
	var value string
	if err := reopened.QueryRow(`SELECT value FROM settings WHERE key = 'special_password_test'`).Scan(&value); err != nil {
		t.Fatalf("read setting after special-character rekey: %v", err)
	}
	if value != "ok" {
		t.Fatalf("unexpected setting after special-character rekey: %s", value)
	}
}

func TestFTS4SearchIndexesTrackHistoryAndAuditRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "SearchPassword123")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	defer database.Close()

	if !tableExists(t, database, "command_requests_fts") {
		t.Fatalf("command_requests_fts table was not created")
	}
	if !tableExists(t, database, "audit_logs_fts") {
		t.Fatalf("audit_logs_fts table was not created")
	}

	if _, err := database.Exec(`
		INSERT INTO servers (name, host, port, username, ssh_key_id, created_at, updated_at)
		VALUES ('worker-1', '127.0.0.1', 22, 'root', 1, datetime('now'), datetime('now'))`,
	); err != nil {
		t.Fatalf("insert server: %v", err)
	}
	result, err := database.Exec(`
		INSERT INTO command_requests (server_id, command, reason, status, stdout, stderr, created_at)
		VALUES (1, 'docker ps', 'inspect containers', 'completed', 'nginx container output', '', datetime('now'))`,
	)
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	commandID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("command id: %v", err)
	}
	assertFTSMatchCount(t, database, "command_requests_fts", "nginx", 1)

	if _, err := database.Exec(`UPDATE command_requests SET stdout = 'postgres container output' WHERE id = ?`, commandID); err != nil {
		t.Fatalf("update command request: %v", err)
	}
	assertFTSMatchCount(t, database, "command_requests_fts", "postgres", 1)
	assertFTSMatchCount(t, database, "command_requests_fts", "nginx", 0)

	if _, err := database.Exec(`
		INSERT INTO audit_logs (actor_type, server_id, action, payload_json, created_at)
		VALUES ('user', 1, 'docker.audit', '{"detail":"image scan finished"}', datetime('now'))`,
	); err != nil {
		t.Fatalf("insert audit log: %v", err)
	}
	assertFTSMatchCount(t, database, "audit_logs_fts", "image", 1)

	if _, err := database.Exec(`DELETE FROM command_requests WHERE id = ?`, commandID); err != nil {
		t.Fatalf("delete command request: %v", err)
	}
	assertFTSMatchCount(t, database, "command_requests_fts", "postgres", 0)
}

func TestSnapshotCreatesConsistentEncryptedCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secure.db")
	snapshotPath := filepath.Join(dir, "snapshots", "secure-copy.aipdb")
	password := "SnapshotPassword123"
	database, err := OpenEncrypted(path, password)
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	defer database.Close()
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('snapshot_test', 'ok', datetime('now'))`); err != nil {
		t.Fatalf("insert setting: %v", err)
	}
	if err := Snapshot(database, snapshotPath); err != nil {
		t.Fatalf("snapshot encrypted db: %v", err)
	}
	if LooksLikePlainSQLite(snapshotPath) {
		t.Fatalf("snapshot should remain encrypted")
	}
	snapshot, err := OpenEncrypted(snapshotPath, password)
	if err != nil {
		t.Fatalf("open encrypted snapshot: %v", err)
	}
	defer snapshot.Close()
	var value string
	if err := snapshot.QueryRow(`SELECT value FROM settings WHERE key = 'snapshot_test'`).Scan(&value); err != nil {
		t.Fatalf("read snapshot setting: %v", err)
	}
	if value != "ok" {
		t.Fatalf("unexpected snapshot value: %s", value)
	}
}

func assertFTSMatchCount(t *testing.T, database *sql.DB, table string, query string, expected int) {
	t.Helper()
	var count int
	sqlQuery := "SELECT COUNT(*) FROM " + table + " WHERE " + table + " MATCH ?"
	if err := database.QueryRow(sqlQuery, query).Scan(&count); err != nil {
		t.Fatalf("query %s match %q: %v", table, query, err)
	}
	if count != expected {
		t.Fatalf("expected %d %s matches for %q, got %d", expected, table, query, count)
	}
}

func tableExists(t *testing.T, database *sql.DB, table string) bool {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count); err != nil {
		t.Fatalf("query table %s: %v", table, err)
	}
	return count == 1
}

func columnExists(t *testing.T, database *sql.DB, table string, column string) bool {
	t.Helper()
	rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("query columns for %s: %v", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan column for %s: %v", table, err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns for %s: %v", table, err)
	}
	return false
}
