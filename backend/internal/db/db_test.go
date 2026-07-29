package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
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
	if !tableExists(t, database, "history_entries") {
		t.Fatalf("history_entries table was not created")
	}
	if !tableExists(t, database, "history_entry_labels") {
		t.Fatalf("history_entry_labels table was not created")
	}
	if !tableExists(t, database, "file_transfers") {
		t.Fatalf("file_transfers table was not created")
	}
	if !tableExists(t, database, "vault_items") {
		t.Fatalf("vault_items table was not created")
	}
	if !tableExists(t, database, "token_project_capabilities") {
		t.Fatalf("token_project_capabilities table was not created")
	}
	if !tableExists(t, database, "token_project_capability_revisions") {
		t.Fatalf("token_project_capability_revisions table was not created")
	}
	if !tableExists(t, database, "file_transfer_batches") {
		t.Fatalf("file_transfer_batches table was not created")
	}
	if !tableExists(t, database, "backup_providers") {
		t.Fatalf("backup_providers table was not created")
	}
	if !tableExists(t, database, "backup_records") {
		t.Fatalf("backup_records table was not created")
	}
	if !columnExists(t, database, "api_tokens", "expires_at") {
		t.Fatalf("api_tokens.expires_at column was not created")
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
	for _, column := range []string{"approval_context", "approval_context_hash", "approval_context_drift"} {
		if columnExists(t, database, "command_requests", column) {
			t.Fatalf("command_requests.%s should not exist in connector-native baseline", column)
		}
	}
	if !columnExists(t, database, "file_transfer_batches", "approval_note") {
		t.Fatalf("file_transfer_batches.approval_note column was not created")
	}
	if !columnExists(t, database, "file_transfer_batches", "overwrite") {
		t.Fatalf("file_transfer_batches.overwrite column was not created")
	}
	for _, table := range []string{
		"connector_targets",
		"connector_credential_profiles",
		"token_connector_action_permissions",
		"connector_action_requests",
	} {
		if !tableExists(t, database, table) {
			t.Fatalf("%s table was not created", table)
		}
	}
	if !columnExists(t, database, "connector_targets", "config_json") {
		t.Fatalf("connector_targets.config_json column was not created")
	}
	if !columnExists(t, database, "connector_credential_profiles", "encrypted_secret_json") {
		t.Fatalf("connector_credential_profiles.encrypted_secret_json column was not created")
	}
	if !columnExists(t, database, "token_connector_action_permissions", "action_name") {
		t.Fatalf("token_connector_action_permissions.action_name column was not created")
	}
	if !columnExists(t, database, "connector_action_requests", "approval_context_hash") {
		t.Fatalf("connector_action_requests.approval_context_hash column was not created")
	}
	if !columnExists(t, database, "connector_action_requests", "source") {
		t.Fatalf("connector_action_requests.source column was not created")
	}
	for _, column := range []string{"title", "summary", "preview_json"} {
		if !columnExists(t, database, "connector_action_requests", column) {
			t.Fatalf("connector_action_requests.%s column was not created", column)
		}
	}
	if !columnExists(t, database, "history_entries", "preview_json") {
		t.Fatalf("history_entries.preview_json column was not created")
	}
	for _, column := range []string{"connector_kind", "target_id", "profile_id", "action_request_id"} {
		if !columnExists(t, database, "audit_logs", column) {
			t.Fatalf("audit_logs.%s column was not created", column)
		}
	}
	var connectorTriggerSQL string
	if err := database.QueryRow(`SELECT COALESCE(group_concat(sql, char(10)), '') FROM sqlite_master WHERE type = 'trigger' AND name LIKE '%connector%'`).Scan(&connectorTriggerSQL); err != nil {
		t.Fatalf("read connector trigger sql: %v", err)
	}
	if strings.Contains(connectorTriggerSQL, "upload_files") {
		t.Fatalf("ssh permission mirror trigger should not create unsupported upload_files action:\n%s", connectorTriggerSQL)
	}
	var foreignKeys int
	if err := database.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign keys pragma: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign keys should be enabled for the connection")
	}
	assertConnectorProfileTargetForeignKeys(t, database)
	if LooksLikePlainSQLite(path) {
		t.Fatalf("encrypted database should not have plaintext sqlite header")
	}

	if wrong, err := OpenEncrypted(path, "wrong-password"); err == nil {
		_ = wrong.Close()
		t.Fatalf("expected wrong password to fail")
	}
}

func TestConnectorTargetsRequireProjectIdentity(t *testing.T) {
	database, err := OpenEncrypted(filepath.Join(t.TempDir(), "project-required.db"), "test-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	defer database.Close()
	_, err = database.Exec(`
		INSERT INTO connector_targets (connector_kind, name, config_json, status, created_at, updated_at)
		VALUES ('postgres', 'missing-project', '{}', 'active', datetime('now'), datetime('now'))`)
	if err == nil || !strings.Contains(err.Error(), "project_id is required") {
		t.Fatalf("missing project insert error = %v", err)
	}
}

func TestOpenEncryptedMigratesConnectorNativeBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := openEncrypted(path, "correct-password", false)
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	if err := runSingleMigration(database, migrations[0]); err != nil {
		t.Fatalf("create connector-native baseline: %v", err)
	}
	if tableExists(t, database, "backup_providers") {
		t.Fatalf("backup provider table should not exist before v2 migration")
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close baseline db: %v", err)
	}

	reopened, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("open baseline db with migrations: %v", err)
	}
	defer reopened.Close()
	if !tableExists(t, reopened, "backup_providers") {
		t.Fatalf("backup provider table was not migrated")
	}
	if !tableExists(t, reopened, "backup_records") {
		t.Fatalf("backup records table was not migrated")
	}
	var count int
	if err := reopened.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 2`).Scan(&count); err != nil {
		t.Fatalf("query backup provider migration: %v", err)
	}
	if count != 1 {
		t.Fatalf("backup provider migration was not recorded")
	}
}

func TestVaultGlobalNameMigrationRejectsCrossProjectDuplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vault-duplicate.db")
	database, err := openEncrypted(path, "correct-password", false)
	if err != nil {
		t.Fatalf("open raw encrypted db: %v", err)
	}
	defer database.Close()
	for index := 0; index < 7; index++ {
		if err := runSingleMigration(database, migrations[index]); err != nil {
			t.Fatalf("apply migration %d: %v", migrations[index].version, err)
		}
	}
	if _, err := database.Exec(`
		DROP INDEX idx_vault_items_active_name;
		CREATE UNIQUE INDEX idx_vault_items_active_owner_name
			ON vault_items(owner_project_id, name COLLATE NOCASE)
			WHERE status = 'active';
		INSERT INTO projects (name, slug, status, created_at, updated_at)
			VALUES ('Second Project', 'second-project', 'active', datetime('now'), datetime('now'));
		INSERT INTO vault_items (
			name, owner_project_id, secret_type, last_value_replaced_at, source, created_at, updated_at
		)
			VALUES
			('DUPLICATE_ENV', (SELECT id FROM projects WHERE slug = 'ungrouped'), 'generic_secret', datetime('now'), 'imported', datetime('now'), datetime('now')),
			('duplicate_env', (SELECT id FROM projects WHERE slug = 'second-project'), 'generic_secret', datetime('now'), 'imported', datetime('now'), datetime('now'));`,
	); err != nil {
		t.Fatalf("prepare pre-v8 duplicate data: %v", err)
	}
	err = runSingleMigration(database, migrations[7])
	if err == nil || !strings.Contains(err.Error(), "active Vault item names must be globally unique") ||
		!strings.Contains(strings.ToLower(err.Error()), "duplicate_env") {
		t.Fatalf("global Vault name migration error = %v", err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = 8`).Scan(&count); err != nil {
		t.Fatalf("read failed migration state: %v", err)
	}
	if count != 0 {
		t.Fatalf("failed migration must not be recorded")
	}
}

func TestOpenEncryptedRejectsPre02PreviewSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := openEncrypted(path, "correct-password", false)
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	if _, err := database.Exec(`CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create schema_migrations: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO schema_migrations (version, description, applied_at) VALUES (1, 'initial schema', datetime('now'))`); err != nil {
		t.Fatalf("insert pre-0.2 preview migration: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := OpenEncrypted(path, "correct-password")
	if err == nil {
		_ = reopened.Close()
		t.Fatalf("expected pre-0.2 preview database to be rejected")
	}
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("expected unsupported schema sentinel, got %v", err)
	}
	if !strings.Contains(err.Error(), "pre-0.2") {
		t.Fatalf("expected pre-0.2 error, got %v", err)
	}
	if message := UnsupportedSchemaMessage(err); !strings.Contains(message, "pre-0.2") {
		t.Fatalf("expected user-facing unsupported schema message, got %q", message)
	}
}

func TestOpenEncryptedMarksRunningConnectorActionsAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	targetID, profileID := insertConnectorTargetAndProfile(t, database)
	if _, err := database.Exec(`
		INSERT INTO connector_action_requests (
			target_id, profile_id, connector_kind, action_name, status, created_at
		)
		VALUES (?, ?, 'postgres', 'query_readonly', 'running', datetime('now'))`,
		targetID,
		profileID,
	); err != nil {
		t.Fatalf("insert running connector action: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("reopen encrypted db: %v", err)
	}
	defer reopened.Close()
	var status, message string
	if err := reopened.QueryRow(`SELECT status, error FROM connector_action_requests LIMIT 1`).Scan(&status, &message); err != nil {
		t.Fatalf("read connector action: %v", err)
	}
	if status != "error" {
		t.Fatalf("expected restarted connector action to be error, got %q", status)
	}
	if message != "gateway restarted while connector action was running" {
		t.Fatalf("unexpected error message: %q", message)
	}
	if err := reopened.QueryRow(`SELECT status, error FROM history_entries WHERE source_ref_type = 'connector_action_request' LIMIT 1`).Scan(&status, &message); err != nil {
		t.Fatalf("read connector action history entry: %v", err)
	}
	if status != "error" {
		t.Fatalf("expected restarted connector action history entry to be error, got %q", status)
	}
	if message != "gateway restarted while connector action was running" {
		t.Fatalf("unexpected history entry error message: %q", message)
	}
}

func TestOpenEncryptedClosesVaultRuntimeStateAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secure.db")
	database, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("open encrypted db: %v", err)
	}
	targetID, profileID := insertConnectorTargetAndProfile(t, database)
	runtimeID := insertConnectorRuntimeSurface(t, database, "postgres", targetID, profileID, "live_console")
	tokenResult, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('vault-restart-token', 'vault-restart-hash', 'aip_restart', datetime('now'), datetime('now'))`)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	tokenID, err := tokenResult.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	var projectID int64
	if err := database.QueryRow(`SELECT project_id FROM connector_targets WHERE id = ?`, targetID).Scan(&projectID); err != nil {
		t.Fatalf("read target project: %v", err)
	}
	sessionResult, err := database.Exec(`
		INSERT INTO console_sessions (
			runtime_id, generation, principal_kind, principal_token_id, workspace_id,
			runtime_instance_id, environment_content_hash, approval_context_hash,
			name, status, created_at, updated_at
		)
		VALUES (?, 1, 'mcp_token', ?, 'workspace', 'runtime-instance', 'environment-hash',
			'approval-hash', 'vault restart session', 'connected', datetime('now'), datetime('now'))`,
		runtimeID,
		tokenID,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := sessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("session id: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO vault_action_requests (
			token_id, project_id, runtime_id, action_name, status, approval_context_hash,
			idempotency_key, expires_at, created_at, updated_at
		)
		VALUES (?, ?, ?, 'restart_session_with_environment', 'running', 'approval-hash',
			'vault-restart-request', datetime('now', '+15 minutes'), datetime('now'), datetime('now'))`,
		tokenID,
		projectID,
		runtimeID,
	); err != nil {
		t.Fatalf("insert running Vault request: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO vault_session_leases (
			token_id, project_id, runtime_id, session_id, session_generation,
			approval_context_hash, environment_content_hash, status, expires_at, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, 1, 'approval-hash', 'environment-hash', 'active',
			datetime('now', '+1 hour'), datetime('now'), datetime('now'))`,
		tokenID,
		projectID,
		runtimeID,
		sessionID,
	); err != nil {
		t.Fatalf("insert active Vault lease: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	reopened, err := OpenEncrypted(path, "correct-password")
	if err != nil {
		t.Fatalf("reopen encrypted db: %v", err)
	}
	defer reopened.Close()
	var requestStatus, requestError string
	if err := reopened.QueryRow(`SELECT status, error FROM vault_action_requests LIMIT 1`).Scan(&requestStatus, &requestError); err != nil {
		t.Fatalf("read Vault request: %v", err)
	}
	if requestStatus != "failed" || requestError != "gateway restarted while the Vault action was running" {
		t.Fatalf("unexpected restarted Vault request: status=%q error=%q", requestStatus, requestError)
	}
	var historyStatus, historyError string
	if err := reopened.QueryRow(`
		SELECT status, error
		FROM history_entries
		WHERE source_ref_type = 'vault_action_request'
		LIMIT 1`).Scan(&historyStatus, &historyError); err != nil {
		t.Fatalf("read Vault request history: %v", err)
	}
	if historyStatus != "failed" || historyError != "gateway restarted while the Vault action was running" {
		t.Fatalf("unexpected restarted Vault history: status=%q error=%q", historyStatus, historyError)
	}
	var leaseStatus string
	if err := reopened.QueryRow(`SELECT status FROM vault_session_leases LIMIT 1`).Scan(&leaseStatus); err != nil {
		t.Fatalf("read Vault lease: %v", err)
	}
	if leaseStatus != "revoked" {
		t.Fatalf("expected restarted Vault lease to be revoked, got %q", leaseStatus)
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

	targetID, profileID := insertConnectorTargetAndProfile(t, database)
	runtimeID := insertConnectorRuntimeSurface(t, database, "postgres", targetID, profileID, "structured_activity")
	result, err := database.Exec(`
		INSERT INTO command_requests (runtime_id, command, reason, status, stdout, stderr, created_at)
		VALUES (?, 'docker ps', 'inspect containers', 'completed', 'nginx container output', '', datetime('now'))`,
		runtimeID,
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
		INSERT INTO audit_logs (actor_type, runtime_id, action, payload_json, created_at)
		VALUES ('user', ?, 'docker.audit', '{"detail":"image scan finished"}', datetime('now'))`,
		runtimeID,
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

func assertConnectorProfileTargetForeignKeys(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('codex', 'hash', 'aip_xxx', datetime('now'), datetime('now'))`,
	); err != nil {
		t.Fatalf("insert token: %v", err)
	}
	firstTargetID, firstProfileID := insertConnectorTargetAndProfile(t, database)
	secondTargetID, _ := insertConnectorTargetAndProfile(t, database)
	if _, err := database.Exec(`
		INSERT INTO token_connector_action_permissions (
			token_id, target_id, profile_id, action_name, execution_rule, created_at, updated_at
		)
		VALUES (1, ?, ?, 'query_readonly', 'approval_required', datetime('now'), datetime('now'))`,
		firstTargetID,
		firstProfileID,
	); err != nil {
		t.Fatalf("insert connector action permission: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO token_connector_action_permissions (
			token_id, target_id, profile_id, action_name, execution_rule, created_at, updated_at
		)
		VALUES (1, ?, ?, 'query_readonly', 'approval_required', datetime('now'), datetime('now'))`,
		secondTargetID,
		firstProfileID,
	); err == nil {
		t.Fatalf("expected mismatched connector profile/target permission to fail")
	}
	if _, err := database.Exec(`
		INSERT INTO connector_action_requests (
			target_id, profile_id, connector_kind, action_name, status, created_at
		)
		VALUES (?, ?, 'postgres', 'query_readonly', 'running', datetime('now'))`,
		secondTargetID,
		firstProfileID,
	); err == nil {
		t.Fatalf("expected mismatched connector profile/target request to fail")
	}
}

func insertConnectorTargetAndProfile(t *testing.T, database *sql.DB) (int64, int64) {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO connector_targets (project_id, connector_kind, name, config_json, created_at, updated_at)
		VALUES (
			(SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active'),
			'postgres', 'postgres-' || lower(hex(randomblob(4))), '{"host":"127.0.0.1"}', datetime('now'), datetime('now')
		)`,
	)
	if err != nil {
		t.Fatalf("insert connector target: %v", err)
	}
	targetID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("target id: %v", err)
	}
	result, err = database.Exec(`
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json, created_at, updated_at
		)
		VALUES (?, 'postgres', 'username_password', 'readonly', '{"username":"app_readonly"}', 'encrypted', datetime('now'), datetime('now'))`,
		targetID,
	)
	if err != nil {
		t.Fatalf("insert connector profile: %v", err)
	}
	profileID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("profile id: %v", err)
	}
	return targetID, profileID
}

func insertConnectorRuntimeSurface(t *testing.T, database *sql.DB, connectorKind string, targetID int64, profileID int64, capabilityKind string) int64 {
	t.Helper()
	result, err := database.Exec(`
		INSERT INTO connector_runtime_surfaces (
			connector_kind, target_id, profile_id, capability_kind, label, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		connectorKind,
		targetID,
		profileID,
		capabilityKind,
		capabilityKind,
	)
	if err != nil {
		t.Fatalf("insert connector runtime surface: %v", err)
	}
	runtimeID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("runtime surface id: %v", err)
	}
	return runtimeID
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
