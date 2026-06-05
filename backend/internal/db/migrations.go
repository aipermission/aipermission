package db

import (
	"database/sql"
	"fmt"
)

type migration struct {
	version     int
	description string
	statements  []string
}

var baseSchemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS servers (
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
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS ssh_keys (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		key_type TEXT NOT NULL CHECK (key_type IN ('ed25519', 'rsa', 'ecdsa')),
		public_key TEXT NOT NULL,
		encrypted_private_key TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS api_tokens (
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
	`CREATE TABLE IF NOT EXISTS token_server_permissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER NOT NULL,
		server_id INTEGER NOT NULL,
		execution_rule TEXT NOT NULL CHECK (execution_rule IN ('always_run', 'approval_required', 'blocked')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(token_id, server_id),
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE IF NOT EXISTS command_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER,
		server_id INTEGER NOT NULL,
		command TEXT NOT NULL,
		encrypted_command TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		stdout TEXT NOT NULL DEFAULT '',
		stderr TEXT NOT NULL DEFAULT '',
		exit_code INTEGER,
		session_id INTEGER,
		user_note TEXT,
		error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		completed_at TEXT,
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
		FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE IF NOT EXISTS console_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		status TEXT NOT NULL,
		transcript TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		cols INTEGER NOT NULL DEFAULT 120,
		rows INTEGER NOT NULL DEFAULT 32,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		closed_at TEXT,
		FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE IF NOT EXISTS console_session_chunks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER NOT NULL,
		seq INTEGER NOT NULL,
		data TEXT NOT NULL,
		created_at TEXT NOT NULL,
		UNIQUE(session_id, seq),
		FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE IF NOT EXISTS message_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER NOT NULL,
		server_id INTEGER,
		session_id INTEGER,
		direction TEXT NOT NULL DEFAULT 'user_to_ai',
		message TEXT NOT NULL,
		consumed_at TEXT,
		created_at TEXT NOT NULL,
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE,
		FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE SET NULL
	);`,
	`CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_type TEXT NOT NULL,
		token_id INTEGER,
		server_id INTEGER,
		action TEXT NOT NULL,
		payload_json TEXT NOT NULL DEFAULT '{}',
		created_at TEXT NOT NULL,
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
		FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE SET NULL
	);`,
	`CREATE TABLE IF NOT EXISTS redaction_rules (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		pattern TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
}

var historyLabelStatements = []string{
	`CREATE TABLE IF NOT EXISTS history_labels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL COLLATE NOCASE UNIQUE,
		color TEXT NOT NULL DEFAULT '#0f766e',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS command_request_labels (
		command_request_id INTEGER NOT NULL,
		label_id INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY(command_request_id, label_id),
		FOREIGN KEY(command_request_id) REFERENCES command_requests(id) ON DELETE CASCADE,
		FOREIGN KEY(label_id) REFERENCES history_labels(id) ON DELETE CASCADE
	);`,
}

var commonIndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_servers_name ON servers(name);`,
	`CREATE INDEX IF NOT EXISTS idx_ssh_keys_name ON ssh_keys(name);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_name ON api_tokens(name);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_expires_at ON api_tokens(expires_at);`,
	`CREATE INDEX IF NOT EXISTS idx_token_server_permissions_token ON token_server_permissions(token_id);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_status ON command_requests(status);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_token_status_created ON command_requests(token_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_created_at ON command_requests(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_server_status_created ON command_requests(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_console_sessions_server ON console_sessions(server_id);`,
	`CREATE INDEX IF NOT EXISTS idx_console_sessions_status ON console_sessions(status);`,
	`CREATE INDEX IF NOT EXISTS idx_console_session_chunks_session_seq ON console_session_chunks(session_id, seq);`,
	`CREATE INDEX IF NOT EXISTS idx_message_queue_token ON message_queue(token_id);`,
	`CREATE INDEX IF NOT EXISTS idx_message_queue_server ON message_queue(server_id);`,
	`CREATE INDEX IF NOT EXISTS idx_message_queue_token_direction_consumed_server ON message_queue(token_id, direction, consumed_at, server_id);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created ON audit_logs(actor_type, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_server_created ON audit_logs(server_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_redaction_rules_enabled ON redaction_rules(enabled);`,
}

var historyLabelIndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_command_request_labels_label ON command_request_labels(label_id);`,
}

var searchIndexStatements = []string{
	`CREATE VIRTUAL TABLE IF NOT EXISTS command_requests_fts USING fts4(command, reason, status, stdout, stderr, error, tokenize=unicode61);`,
	`INSERT OR REPLACE INTO command_requests_fts(rowid, command, reason, status, stdout, stderr, error)
		SELECT id, command, reason, status, stdout, stderr, error FROM command_requests;`,
	`CREATE TRIGGER IF NOT EXISTS command_requests_fts_ai AFTER INSERT ON command_requests BEGIN
		INSERT OR REPLACE INTO command_requests_fts(rowid, command, reason, status, stdout, stderr, error)
		VALUES (new.id, new.command, new.reason, new.status, new.stdout, new.stderr, new.error);
	END;`,
	`CREATE TRIGGER IF NOT EXISTS command_requests_fts_au AFTER UPDATE ON command_requests BEGIN
		DELETE FROM command_requests_fts WHERE rowid = old.id;
		INSERT OR REPLACE INTO command_requests_fts(rowid, command, reason, status, stdout, stderr, error)
		VALUES (new.id, new.command, new.reason, new.status, new.stdout, new.stderr, new.error);
	END;`,
	`CREATE TRIGGER IF NOT EXISTS command_requests_fts_ad AFTER DELETE ON command_requests BEGIN
		DELETE FROM command_requests_fts WHERE rowid = old.id;
	END;`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS audit_logs_fts USING fts4(actor_type, action, payload_json, tokenize=unicode61);`,
	`INSERT OR REPLACE INTO audit_logs_fts(rowid, actor_type, action, payload_json)
		SELECT id, actor_type, action, payload_json FROM audit_logs;`,
	`CREATE TRIGGER IF NOT EXISTS audit_logs_fts_ai AFTER INSERT ON audit_logs BEGIN
		INSERT OR REPLACE INTO audit_logs_fts(rowid, actor_type, action, payload_json)
		VALUES (new.id, new.actor_type, new.action, new.payload_json);
	END;`,
	`CREATE TRIGGER IF NOT EXISTS audit_logs_fts_au AFTER UPDATE ON audit_logs BEGIN
		DELETE FROM audit_logs_fts WHERE rowid = old.id;
		INSERT OR REPLACE INTO audit_logs_fts(rowid, actor_type, action, payload_json)
		VALUES (new.id, new.actor_type, new.action, new.payload_json);
	END;`,
	`CREATE TRIGGER IF NOT EXISTS audit_logs_fts_ad AFTER DELETE ON audit_logs BEGIN
		DELETE FROM audit_logs_fts WHERE rowid = old.id;
	END;`,
}

var ecdsaSSHKeyStatements = []string{
	`DROP TABLE IF EXISTS ssh_keys_next;`,
	`CREATE TABLE IF NOT EXISTS ssh_keys_next (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL UNIQUE,
		key_type TEXT NOT NULL CHECK (key_type IN ('ed25519', 'rsa', 'ecdsa')),
		public_key TEXT NOT NULL,
		encrypted_private_key TEXT NOT NULL,
		fingerprint TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`INSERT INTO ssh_keys_next (id, name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at)
		SELECT id, name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at FROM ssh_keys;`,
	`DROP TABLE ssh_keys;`,
	`ALTER TABLE ssh_keys_next RENAME TO ssh_keys;`,
	`CREATE INDEX IF NOT EXISTS idx_ssh_keys_name ON ssh_keys(name);`,
}

var manualHistoryGroundworkStatements = []string{
	`ALTER TABLE command_requests ADD COLUMN source TEXT NOT NULL DEFAULT 'mcp';`,
	`ALTER TABLE command_requests ADD COLUMN tracking_reason TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE command_requests ADD COLUMN output_truncated INTEGER NOT NULL DEFAULT 0;`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_source_created ON command_requests(source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_server_source_created ON command_requests(server_id, source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_token_source_status_created ON command_requests(token_id, source, status, created_at);`,
}

var fileTransferStatements = []string{
	`CREATE TABLE IF NOT EXISTS file_transfers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER NOT NULL,
		direction TEXT NOT NULL CHECK (direction IN ('upload', 'download')),
		source TEXT NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'mcp')),
		status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'canceled')),
		local_path TEXT NOT NULL DEFAULT '',
		remote_path TEXT NOT NULL,
		file_name TEXT NOT NULL DEFAULT '',
		size_bytes INTEGER NOT NULL DEFAULT 0,
		transferred_bytes INTEGER NOT NULL DEFAULT 0,
		checksum_sha256 TEXT NOT NULL DEFAULT '',
		temp_path TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		started_at TEXT,
		completed_at TEXT,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(server_id) REFERENCES servers(id) ON DELETE CASCADE
	);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_created ON file_transfers(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_server_status_created ON file_transfers(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_direction_created ON file_transfers(direction, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_status_created ON file_transfers(status, created_at);`,
}

var migrations = []migration{
	{
		version:     1,
		description: "initial schema",
		statements:  sqlStatements(baseSchemaStatements, commonIndexStatements, searchIndexStatements),
	},
	{
		version:     2,
		description: "history labels",
		statements:  sqlStatements(historyLabelStatements, historyLabelIndexStatements),
	},
	{
		version:     3,
		description: "allow imported ecdsa ssh keys",
		statements:  ecdsaSSHKeyStatements,
	},
	{
		version:     4,
		description: "manual history groundwork",
		statements:  manualHistoryGroundworkStatements,
	},
	{
		version:     5,
		description: "file transfer history",
		statements:  fileTransferStatements,
	},
}

func sqlStatements(groups ...[]string) []string {
	var total int
	for _, group := range groups {
		total += len(group)
	}
	statements := make([]string, 0, total)
	for _, group := range groups {
		statements = append(statements, group...)
	}
	return statements
}

func migrate(database *sql.DB) error {
	if _, err := database.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable sqlite foreign keys: %w", err)
	}
	if err := ensureMigrationTable(database); err != nil {
		return err
	}
	if err := runSchemaMigrations(database); err != nil {
		return err
	}
	if err := ensureCurrentSchema(database); err != nil {
		return err
	}
	if err := runMigrationMaintenance(database); err != nil {
		return err
	}
	return nil
}

func ensureCurrentSchema(database *sql.DB) error {
	for _, statement := range sqlStatements(historyLabelStatements, historyLabelIndexStatements, fileTransferStatements) {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("ensure current schema: %w", err)
		}
	}
	return nil
}

func ensureMigrationTable(database *sql.DB) error {
	_, err := database.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		description TEXT NOT NULL,
		applied_at TEXT NOT NULL
	);`)
	if err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}
	return nil
}

func runSchemaMigrations(database *sql.DB) error {
	for _, migration := range migrations {
		applied, err := migrationApplied(database, migration.version)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if err := runSingleMigration(database, migration); err != nil {
			return err
		}
	}
	return nil
}

func migrationApplied(database *sql.DB, version int) (bool, error) {
	var exists int
	err := database.QueryRow(`SELECT 1 FROM schema_migrations WHERE version = ?`, version).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if err == sql.ErrNoRows {
		return false, nil
	}
	return false, fmt.Errorf("read schema migration %d: %w", version, err)
}

func runSingleMigration(database *sql.DB, migration migration) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", migration.version, err)
	}
	defer tx.Rollback()

	for _, statement := range migration.statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("run migration %d: %w", migration.version, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_migrations (version, description, applied_at) VALUES (?, ?, datetime('now'))`,
		migration.version,
		migration.description,
	); err != nil {
		return fmt.Errorf("record schema migration %d: %w", migration.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %d: %w", migration.version, err)
	}
	return nil
}

func runMigrationMaintenance(database *sql.DB) error {
	for _, statement := range []string{
		`UPDATE console_sessions SET status = 'closed', error = 'gateway restarted', closed_at = COALESCE(closed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('connecting', 'connected')`,
		`UPDATE command_requests SET status = 'error', error = 'gateway restarted while command was running', completed_at = COALESCE(completed_at, datetime('now')) WHERE status = 'running'`,
	} {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("run migration maintenance: %w", err)
		}
	}
	return nil
}
