package migration

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestMigrateLegacy010To020CopiesMinimumSSHConfiguration(t *testing.T) {
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	sourceID, sourcePath, err := db.NewDatabasePath(dataPath, "Legacy")
	if err != nil {
		t.Fatalf("source path: %v", err)
	}
	sourcePassword := "LegacyPassword123"
	sourceSecret := "migration-test-secret-01234567890123456789"
	sourceDB, err := db.OpenEncryptedForMigration(sourcePath, sourcePassword)
	if err != nil {
		t.Fatalf("open source db: %v", err)
	}
	createLegacySchema(t, sourceDB)
	insertLegacyRows(t, sourceDB, sourceSecret)
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}

	result, err := MigrateLegacy010To020(ctx, Legacy010To020Request{
		DataPath:         dataPath,
		FallbackSecret:   sourceSecret,
		SourceDatabaseID: sourceID,
		SourcePassword:   sourcePassword,
		TargetName:       "Migrated",
		TargetPassword:   "MigratedPassword123",
	})
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	if result.SSHKeys != 1 || result.Targets != 1 || result.Tokens != 1 || result.Permissions != 1 {
		t.Fatalf("unexpected migration counts: %+v", result)
	}

	targetPath, err := db.DatabasePath(dataPath, result.TargetDatabaseID)
	if err != nil {
		t.Fatalf("target path: %v", err)
	}
	targetDB, err := db.OpenEncrypted(targetPath, "MigratedPassword123")
	if err != nil {
		t.Fatalf("open target db: %v", err)
	}
	defer targetDB.Close()

	assertCount(t, targetDB, `SELECT COUNT(*) FROM connector_credential_resources WHERE connector_kind = 'ssh' AND resource_kind = 'private_key'`, 1)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM connector_targets WHERE connector_kind = 'ssh' AND name = 'core-1'`, 1)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM connector_credential_profiles WHERE connector_kind = 'ssh' AND label = 'root'`, 1)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM connector_runtime_surfaces WHERE connector_kind = 'ssh' AND capability_kind = 'live_console'`, 1)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM api_tokens WHERE name = 'codex' AND id = 7`, 1)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM token_connector_action_permissions WHERE token_id = 7 AND action_name = 'exec' AND execution_rule = 'always_run'`, 1)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM command_requests`, 0)
	assertCount(t, targetDB, `SELECT COUNT(*) FROM audit_logs`, 0)

	secretVault, err := vault.New(sourceSecret)
	if err != nil {
		t.Fatalf("create target vault: %v", err)
	}
	privateKey, err := sshkeys.NewStore(targetDB, secretVault).GetPrivateKey(ctx, 1)
	if err != nil {
		t.Fatalf("read migrated private key: %v", err)
	}
	if privateKey.PrivateKey == "" {
		t.Fatalf("migrated private key should decrypt")
	}
}

func TestMigrateLegacy010To020KeepsOldSourcePasswordButRequiresStrongNewPassword(t *testing.T) {
	ctx := context.Background()
	dataPath := filepath.Join(t.TempDir(), "aipermission.db")
	sourceID, sourcePath, err := db.NewDatabasePath(dataPath, "Legacy")
	if err != nil {
		t.Fatalf("source path: %v", err)
	}
	sourceSecret := "migration-test-secret-01234567890123456789"
	sourceDB, err := db.OpenEncryptedForMigration(sourcePath, "oldpass")
	if err != nil {
		t.Fatalf("open source db with old password shape: %v", err)
	}
	createLegacySchema(t, sourceDB)
	insertLegacyRows(t, sourceDB, sourceSecret)
	if err := sourceDB.Close(); err != nil {
		t.Fatalf("close source db: %v", err)
	}

	if _, err := MigrateLegacy010To020(ctx, Legacy010To020Request{
		DataPath:         dataPath,
		FallbackSecret:   sourceSecret,
		SourceDatabaseID: sourceID,
		SourcePassword:   "oldpass",
		TargetName:       "Migrated",
		TargetPassword:   "weak",
	}); err == nil {
		t.Fatalf("migration should reject weak new database password")
	}

	if _, err := MigrateLegacy010To020(ctx, Legacy010To020Request{
		DataPath:         dataPath,
		FallbackSecret:   sourceSecret,
		SourceDatabaseID: sourceID,
		SourcePassword:   "oldpass",
		TargetName:       "Migrated Strong",
		TargetPassword:   "StrongPassword123",
	}); err != nil {
		t.Fatalf("migration should accept old source password with strong new password: %v", err)
	}
}

func createLegacySchema(t *testing.T, database *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL);`,
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, description TEXT NOT NULL, applied_at TEXT NOT NULL);`,
		`CREATE TABLE ssh_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			key_type TEXT NOT NULL,
			public_key TEXT NOT NULL,
			encrypted_private_key TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE servers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			host TEXT NOT NULL,
			port INTEGER NOT NULL DEFAULT 22,
			username TEXT NOT NULL,
			ssh_key_id INTEGER NOT NULL DEFAULT 0,
			auth_type TEXT NOT NULL DEFAULT 'private_key',
			key_label TEXT NOT NULL DEFAULT '',
			encrypted_secret TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			startup_input_after_connect TEXT NOT NULL DEFAULT '',
			force_shell_command TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE api_tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			token_hash TEXT NOT NULL UNIQUE,
			token_prefix TEXT NOT NULL,
			token_value TEXT NOT NULL DEFAULT '',
			revoked_at TEXT,
			expires_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE token_server_permissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_id INTEGER NOT NULL,
			server_id INTEGER NOT NULL,
			execution_rule TEXT NOT NULL,
			expires_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE redaction_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			pattern TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE history_labels (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			color TEXT NOT NULL DEFAULT '#0f766e',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE command_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_id INTEGER,
			server_id INTEGER NOT NULL,
			command TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE audit_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			actor_type TEXT NOT NULL,
			action TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,
	} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("create legacy schema: %v", err)
		}
	}
}

func insertLegacyRows(t *testing.T, database *sql.DB, secret string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	secretVault, err := vault.New(secret)
	if err != nil {
		t.Fatalf("create source vault: %v", err)
	}
	encryptedPrivateKey, err := secretVault.EncryptJSON(privateKeySecret{PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\ntest\n-----END OPENSSH PRIVATE KEY-----"})
	if err != nil {
		t.Fatalf("encrypt legacy private key: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('gateway_secret', ?, ?)`, secret, now); err != nil {
		t.Fatalf("insert legacy setting: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO schema_migrations (version, description, applied_at) VALUES (13, 'server ssh advanced startup settings', ?)`, now); err != nil {
		t.Fatalf("insert legacy migration row: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO ssh_keys (
			id, name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at
		)
		VALUES (3, 'main', 'ed25519', 'ssh-ed25519 AAAATEST aipermission-main', ?, 'SHA256:test', ?, ?)`,
		encryptedPrivateKey,
		now,
		now,
	); err != nil {
		t.Fatalf("insert legacy ssh key: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO servers (
			id, name, host, port, username, ssh_key_id, description,
			startup_input_after_connect, force_shell_command, created_at, updated_at
		)
		VALUES (5, 'core-1', '10.0.0.10', 22, 'root', ?, 'primary server', 'q', '', ?, ?)`,
		3,
		now,
		now,
	); err != nil {
		t.Fatalf("insert legacy server: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO api_tokens (
			id, name, token_hash, token_prefix, token_value, created_at, updated_at
		)
		VALUES (7, 'codex', 'sha256:test', 'aip_test', '', ?, ?)`,
		now,
		now,
	); err != nil {
		t.Fatalf("insert legacy token: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO token_server_permissions (
			token_id, server_id, execution_rule, created_at, updated_at
		)
		VALUES (7, 5, 'always_run', ?, ?)`,
		now,
		now,
	); err != nil {
		t.Fatalf("insert legacy permission: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO command_requests (server_id, command, status, created_at) VALUES (5, 'whoami', 'completed', ?)`, now); err != nil {
		t.Fatalf("insert legacy history row: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO audit_logs (actor_type, action, payload_json, created_at) VALUES ('user', 'legacy.audit', '{}', ?)`, now); err != nil {
		t.Fatalf("insert legacy audit row: %v", err)
	}
}

func assertCount(t *testing.T, database *sql.DB, query string, want int) {
	t.Helper()
	var got int
	if err := database.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("count query failed: %v", err)
	}
	if got != want {
		t.Fatalf("query %q got %d, want %d", query, got, want)
	}
}
