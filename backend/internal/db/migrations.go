package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

const connectorNativeBaselineDescription = "0.2 connector-native baseline"

var ErrUnsupportedSchema = errors.New("unsupported database schema")

const unsupportedPre02DatabaseMessage = "database uses an unsupported pre-0.2 or non-baseline schema; create a fresh 0.2 database or migrate with the one-time import tool. To migrate a 0.1.x database, run `docker compose --profile migrate up -d --build migration`, then open http://localhost:3211."

func UnsupportedSchemaMessage(err error) string {
	if !errors.Is(err, ErrUnsupportedSchema) {
		return ""
	}
	message := strings.TrimPrefix(err.Error(), ErrUnsupportedSchema.Error()+": ")
	if message == err.Error() {
		return "database uses an unsupported schema"
	}
	return message
}

type migration struct {
	version     int
	description string
	statements  []string
}

var coreTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS connector_credential_resources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		connector_kind TEXT NOT NULL,
		resource_kind TEXT NOT NULL,
		name TEXT NOT NULL,
		resource_type TEXT NOT NULL,
		public_data TEXT NOT NULL DEFAULT '',
		encrypted_secret TEXT NOT NULL DEFAULT '',
		fingerprint TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(connector_kind, resource_kind, name)
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
	`CREATE TABLE IF NOT EXISTS connector_targets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		connector_kind TEXT NOT NULL,
		name TEXT NOT NULL,
		config_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS connector_credential_profiles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		target_id INTEGER NOT NULL,
		connector_kind TEXT NOT NULL,
		kind TEXT NOT NULL,
		label TEXT NOT NULL,
		public_json TEXT NOT NULL DEFAULT '{}',
		encrypted_secret_json TEXT NOT NULL DEFAULT '',
		risk_label TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(id, target_id),
		FOREIGN KEY(target_id) REFERENCES connector_targets(id) ON DELETE RESTRICT
	);`,
	`CREATE TABLE IF NOT EXISTS connector_runtime_surfaces (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		connector_kind TEXT NOT NULL,
		target_id INTEGER NOT NULL,
		profile_id INTEGER NOT NULL,
		capability_kind TEXT NOT NULL,
		label TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(connector_kind, target_id, profile_id, capability_kind),
		FOREIGN KEY(target_id) REFERENCES connector_targets(id) ON DELETE RESTRICT,
		FOREIGN KEY(profile_id, target_id) REFERENCES connector_credential_profiles(id, target_id) ON DELETE RESTRICT
	);`,
	`CREATE TABLE IF NOT EXISTS token_connector_action_permissions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER NOT NULL,
		target_id INTEGER NOT NULL,
		profile_id INTEGER NOT NULL,
		action_name TEXT NOT NULL,
		execution_rule TEXT NOT NULL CHECK (execution_rule IN ('always_run', 'approval_required', 'blocked')),
		expires_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(token_id, target_id, profile_id, action_name),
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(target_id) REFERENCES connector_targets(id) ON DELETE RESTRICT,
		FOREIGN KEY(profile_id, target_id) REFERENCES connector_credential_profiles(id, target_id) ON DELETE RESTRICT
	);`,
	`CREATE TABLE IF NOT EXISTS command_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER,
		runtime_id INTEGER NOT NULL,
		source TEXT NOT NULL DEFAULT 'mcp',
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
			tracking_reason TEXT NOT NULL DEFAULT '',
			output_truncated INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			completed_at TEXT,
			FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE RESTRICT
		);`,
	`CREATE TABLE IF NOT EXISTS console_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		runtime_id INTEGER NOT NULL,
		name TEXT NOT NULL,
		status TEXT NOT NULL,
			transcript TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			cols INTEGER NOT NULL DEFAULT 120,
			rows INTEGER NOT NULL DEFAULT 32,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			closed_at TEXT,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE RESTRICT
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
		runtime_id INTEGER,
			session_id INTEGER,
			direction TEXT NOT NULL DEFAULT 'user_to_ai',
			message TEXT NOT NULL,
			consumed_at TEXT,
			created_at TEXT NOT NULL,
			FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE SET NULL,
			FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE SET NULL
		);`,
	`CREATE TABLE IF NOT EXISTS connector_action_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER,
		target_id INTEGER NOT NULL,
		profile_id INTEGER NOT NULL,
		connector_kind TEXT NOT NULL,
		action_name TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		preview_json TEXT NOT NULL DEFAULT '{}',
		source TEXT NOT NULL DEFAULT 'mcp',
		input_json TEXT NOT NULL DEFAULT '{}',
		encrypted_payload_json TEXT NOT NULL DEFAULT '',
		reason TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		output_json TEXT NOT NULL DEFAULT '{}',
		display_text TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		approval_context TEXT NOT NULL DEFAULT '',
		approval_context_hash TEXT NOT NULL DEFAULT '',
		approval_context_drift TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		completed_at TEXT,
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
		FOREIGN KEY(target_id) REFERENCES connector_targets(id) ON DELETE RESTRICT,
		FOREIGN KEY(profile_id, target_id) REFERENCES connector_credential_profiles(id, target_id) ON DELETE RESTRICT
	);`,
	`CREATE TABLE IF NOT EXISTS audit_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		actor_type TEXT NOT NULL,
		token_id INTEGER,
		runtime_id INTEGER,
		connector_kind TEXT NOT NULL DEFAULT '',
		target_id INTEGER,
		profile_id INTEGER,
			action_request_id INTEGER,
			action TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL,
			FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE SET NULL
		);`,
	`CREATE TABLE IF NOT EXISTS redaction_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			pattern TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
	`CREATE TABLE IF NOT EXISTS history_labels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL COLLATE NOCASE UNIQUE,
		color TEXT NOT NULL DEFAULT '#0f766e',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
}

var fileTransferTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS file_transfer_batches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		runtime_id INTEGER NOT NULL,
		direction TEXT NOT NULL CHECK (direction IN ('upload', 'download')),
		source TEXT NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'mcp')),
		status TEXT NOT NULL CHECK (status IN ('pending', 'pending_approval', 'running', 'paused', 'completed', 'failed', 'canceled')),
		archive_name TEXT NOT NULL DEFAULT '',
		approval_note TEXT NOT NULL DEFAULT '',
		overwrite INTEGER NOT NULL DEFAULT 0,
		archive_path TEXT NOT NULL DEFAULT '',
		total_items INTEGER NOT NULL DEFAULT 0,
		completed_items INTEGER NOT NULL DEFAULT 0,
		failed_items INTEGER NOT NULL DEFAULT 0,
		canceled_items INTEGER NOT NULL DEFAULT 0,
		size_bytes INTEGER NOT NULL DEFAULT 0,
			transferred_bytes INTEGER NOT NULL DEFAULT 0,
			bytes_per_second INTEGER NOT NULL DEFAULT 0,
			eta_seconds INTEGER NOT NULL DEFAULT -1,
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE RESTRICT
		);`,
	`CREATE TABLE IF NOT EXISTS file_transfers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_id INTEGER,
		queue_index INTEGER NOT NULL DEFAULT 0,
		runtime_id INTEGER NOT NULL,
		direction TEXT NOT NULL CHECK (direction IN ('upload', 'download')),
		source TEXT NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'mcp')),
		status TEXT NOT NULL CHECK (status IN ('pending', 'pending_approval', 'running', 'paused', 'completed', 'failed', 'canceled')),
		local_path TEXT NOT NULL DEFAULT '',
		remote_path TEXT NOT NULL,
		file_name TEXT NOT NULL DEFAULT '',
		size_bytes INTEGER NOT NULL DEFAULT 0,
		transferred_bytes INTEGER NOT NULL DEFAULT 0,
		bytes_per_second INTEGER NOT NULL DEFAULT 0,
		eta_seconds INTEGER NOT NULL DEFAULT -1,
		checksum_sha256 TEXT NOT NULL DEFAULT '',
		temp_path TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(batch_id) REFERENCES file_transfer_batches(id) ON DELETE SET NULL,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE RESTRICT
		);`,
}

var historyTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS history_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_ref_type TEXT NOT NULL,
		source_ref_id INTEGER NOT NULL,
		connector_kind TEXT NOT NULL,
		activity_type TEXT NOT NULL,
		token_id INTEGER,
		runtime_id INTEGER,
		target_id INTEGER,
		profile_id INTEGER,
		target_name TEXT NOT NULL DEFAULT '',
		profile_label TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		action_name TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
		preview_json TEXT NOT NULL DEFAULT '{}',
		input_text TEXT NOT NULL DEFAULT '',
		input_json TEXT NOT NULL DEFAULT '{}',
		output_text TEXT NOT NULL DEFAULT '',
		output_json TEXT NOT NULL DEFAULT '{}',
		error TEXT NOT NULL DEFAULT '',
		exit_code INTEGER,
		progress_current INTEGER NOT NULL DEFAULT 0,
		progress_total INTEGER NOT NULL DEFAULT 0,
		bytes_done INTEGER NOT NULL DEFAULT 0,
		bytes_total INTEGER NOT NULL DEFAULT 0,
		approval_required INTEGER NOT NULL DEFAULT 0,
			user_note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			started_at TEXT,
			completed_at TEXT,
			updated_at TEXT NOT NULL,
			UNIQUE(source_ref_type, source_ref_id),
			FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
			FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE SET NULL,
			FOREIGN KEY(target_id) REFERENCES connector_targets(id) ON DELETE SET NULL,
			FOREIGN KEY(profile_id) REFERENCES connector_credential_profiles(id) ON DELETE SET NULL
		);`,
	`CREATE TABLE IF NOT EXISTS history_entry_labels (
		history_entry_id INTEGER NOT NULL,
		label_id INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		PRIMARY KEY(history_entry_id, label_id),
		FOREIGN KEY(history_entry_id) REFERENCES history_entries(id) ON DELETE CASCADE,
		FOREIGN KEY(label_id) REFERENCES history_labels(id) ON DELETE CASCADE
	);`,
}

var indexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_connector_credential_resources_kind_name ON connector_credential_resources(connector_kind, resource_kind, name);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_name ON api_tokens(name);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_expires_at ON api_tokens(expires_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_targets_kind_name ON connector_targets(connector_kind, name);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_targets_active_kind_name ON connector_targets(connector_kind, name) WHERE status = 'active';`,
	`CREATE INDEX IF NOT EXISTS idx_connector_credential_profiles_target ON connector_credential_profiles(target_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_credential_profiles_active_label ON connector_credential_profiles(target_id, label) WHERE status = 'active';`,
	`CREATE INDEX IF NOT EXISTS idx_connector_runtime_surfaces_profile ON connector_runtime_surfaces(profile_id, status);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_runtime_surfaces_target ON connector_runtime_surfaces(target_id, status);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_runtime_surfaces_active ON connector_runtime_surfaces(connector_kind, target_id, profile_id, capability_kind) WHERE status = 'active';`,
	`CREATE INDEX IF NOT EXISTS idx_token_connector_action_permissions_token ON token_connector_action_permissions(token_id);`,
	`CREATE INDEX IF NOT EXISTS idx_token_connector_action_permissions_lookup ON token_connector_action_permissions(token_id, target_id, profile_id, action_name);`,
	`CREATE INDEX IF NOT EXISTS idx_token_connector_action_permissions_expires_at ON token_connector_action_permissions(expires_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_status ON command_requests(status);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_token_status_created ON command_requests(token_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_created_at ON command_requests(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_runtime_status_created ON command_requests(runtime_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_source_created ON command_requests(source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_runtime_source_created ON command_requests(runtime_id, source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_token_source_status_created ON command_requests(token_id, source, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_console_sessions_runtime ON console_sessions(runtime_id);`,
	`CREATE INDEX IF NOT EXISTS idx_console_sessions_status ON console_sessions(status);`,
	`CREATE INDEX IF NOT EXISTS idx_console_session_chunks_session_seq ON console_session_chunks(session_id, seq);`,
	`CREATE INDEX IF NOT EXISTS idx_message_queue_token ON message_queue(token_id);`,
	`CREATE INDEX IF NOT EXISTS idx_message_queue_runtime ON message_queue(runtime_id);`,
	`CREATE INDEX IF NOT EXISTS idx_message_queue_token_direction_consumed_runtime ON message_queue(token_id, direction, consumed_at, runtime_id);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_token_status_created ON connector_action_requests(token_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_target_status_created ON connector_action_requests(target_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_kind_action_created ON connector_action_requests(connector_kind, action_name, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_approval_context_hash ON connector_action_requests(approval_context_hash);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_source_created ON connector_action_requests(source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_created ON audit_logs(actor_type, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_runtime_created ON audit_logs(runtime_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_connector_created ON audit_logs(connector_kind, target_id, profile_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_action_request ON audit_logs(action_request_id);`,
	`CREATE INDEX IF NOT EXISTS idx_redaction_rules_enabled ON redaction_rules(enabled);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_created ON file_transfer_batches(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_runtime_status_created ON file_transfer_batches(runtime_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_created ON file_transfers(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_runtime_status_created ON file_transfers(runtime_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_direction_created ON file_transfers(direction, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_status_created ON file_transfers(status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_batch_queue ON file_transfers(batch_id, queue_index, id);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_batch_status ON file_transfers(batch_id, status);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_created ON history_entries(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_kind_created ON history_entries(connector_kind, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_activity_created ON history_entries(activity_type, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_status_created ON history_entries(status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_target_created ON history_entries(target_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_profile_created ON history_entries(profile_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_runtime_created ON history_entries(runtime_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_source_created ON history_entries(source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entry_labels_label ON history_entry_labels(label_id);`,
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

var historyProjectionStatements = []string{
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, token_id, runtime_id,
		target_id, profile_id, target_name, profile_label, source, status, action_name,
		title, summary, input_text, output_text, error, exit_code, approval_required,
		user_note, created_at, started_at, completed_at, updated_at
	)
	SELECT
		'command_request', cr.id, COALESCE(rs.connector_kind, ''), 'command', cr.token_id, cr.runtime_id,
		ct.id, cp.id, COALESCE(ct.name, ''), COALESCE(cp.label, ''), cr.source, cr.status, 'exec',
		CASE
			WHEN length(cr.command) > 120 THEN substr(cr.command, 1, 117) || '...'
			ELSE cr.command
		END,
		CASE
			WHEN cr.reason != '' THEN cr.reason
			ELSE cr.tracking_reason
		END,
		cr.command,
		trim(cr.stdout || CASE WHEN cr.stderr != '' THEN char(10) || cr.stderr ELSE '' END),
		cr.error,
		cr.exit_code,
		CASE WHEN cr.status = 'pending_approval' THEN 1 ELSE 0 END,
		COALESCE(cr.user_note, ''),
		cr.created_at,
		NULL,
		cr.completed_at,
		COALESCE(cr.completed_at, cr.created_at)
	FROM command_requests cr
	LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
	LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
	LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind;`,
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, token_id, target_id,
		profile_id, target_name, profile_label, source, status, action_name, title, summary,
		preview_json, input_json, output_text, output_json, error, approval_required, created_at,
		completed_at, updated_at
	)
	SELECT
		'connector_action_request', r.id, r.connector_kind, 'action', r.token_id, r.target_id,
		r.profile_id, t.name, p.label, COALESCE(NULLIF(r.source, ''), 'mcp'),
		CASE WHEN r.status = 'approval_pending' THEN 'pending_approval' ELSE r.status END,
		r.action_name, COALESCE(NULLIF(r.title, ''), r.action_name),
		COALESCE(NULLIF(r.summary, ''), r.reason), r.preview_json, r.input_json, r.display_text, r.output_json, r.error,
		CASE WHEN r.status = 'approval_pending' THEN 1 ELSE 0 END,
		r.created_at, r.completed_at, COALESCE(r.completed_at, r.created_at)
	FROM connector_action_requests r
	JOIN connector_targets t ON t.id = r.target_id
	JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id AND p.connector_kind = r.connector_kind;`,
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, runtime_id, target_id,
		profile_id, target_name, profile_label, source, status, action_name, title, summary,
		input_text, input_json, output_text, error, progress_current, progress_total,
		bytes_done, bytes_total, approval_required, created_at, started_at, completed_at,
		updated_at
	)
	SELECT
		'file_transfer', ft.id, COALESCE(rs.connector_kind, ''), 'file_transfer', ft.runtime_id, ct.id, cp.id,
		COALESCE(ct.name, ''), COALESCE(cp.label, ''), ft.source, ft.status, ft.direction,
		ft.direction || ': ' || ft.file_name,
		ft.remote_path,
		ft.direction || ' ' || ft.remote_path,
		'{}',
		CASE
			WHEN ft.checksum_sha256 != '' THEN 'sha256:' || ft.checksum_sha256
			ELSE ''
		END,
		ft.error,
		ft.transferred_bytes,
		ft.size_bytes,
		ft.transferred_bytes,
		ft.size_bytes,
		CASE WHEN ft.status = 'pending_approval' THEN 1 ELSE 0 END,
		ft.created_at,
		ft.started_at,
		ft.completed_at,
		ft.updated_at
	FROM file_transfers ft
	LEFT JOIN connector_runtime_surfaces rs ON rs.id = ft.runtime_id
	LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
	LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind;`,
}

var migrations = []migration{
	{
		version:     currentSchemaVersion,
		description: connectorNativeBaselineDescription,
		statements: sqlStatements(
			coreTableStatements,
			fileTransferTableStatements,
			historyTableStatements,
			indexStatements,
			searchIndexStatements,
		),
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
	if err := rejectUnsupportedPreviewSchema(database); err != nil {
		return err
	}
	if err := runSchemaMigrations(database); err != nil {
		return err
	}
	if err := syncHistoryProjections(database); err != nil {
		return err
	}
	if err := runMigrationMaintenance(database); err != nil {
		return err
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

func rejectUnsupportedPreviewSchema(database *sql.DB) error {
	var migrationCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		return fmt.Errorf("read schema migration count: %w", err)
	}
	if migrationCount == 0 {
		hasExistingSchema, err := hasPreexistingApplicationSchema(database)
		if err != nil {
			return err
		}
		if hasExistingSchema {
			return fmt.Errorf("%w: %s", ErrUnsupportedSchema, unsupportedPre02DatabaseMessage)
		}
		return nil
	}

	var baselineDescription string
	err := database.QueryRow(`SELECT description FROM schema_migrations WHERE version = ?`, currentSchemaVersion).Scan(&baselineDescription)
	if err == sql.ErrNoRows {
		return fmt.Errorf("%w: %s", ErrUnsupportedSchema, unsupportedPre02DatabaseMessage)
	}
	if err != nil {
		return fmt.Errorf("read connector-native baseline migration: %w", err)
	}
	if baselineDescription != connectorNativeBaselineDescription {
		return fmt.Errorf("%w: %s", ErrUnsupportedSchema, unsupportedPre02DatabaseMessage)
	}

	var maxVersion int
	if err := database.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&maxVersion); err != nil {
		return fmt.Errorf("read max schema migration: %w", err)
	}
	if maxVersion > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than this gateway supports", maxVersion)
	}
	return nil
}

func hasPreexistingApplicationSchema(database *sql.DB) (bool, error) {
	var count int
	err := database.QueryRow(`
		SELECT COUNT(*)
		FROM sqlite_master
		WHERE type = 'table'
			AND name NOT IN ('schema_migrations')
			AND name NOT LIKE 'sqlite_%'
	`).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("inspect existing schema: %w", err)
	}
	return count > 0, nil
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

func syncHistoryProjections(database *sql.DB) error {
	for _, statement := range historyProjectionStatements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("sync history projections: %w", err)
		}
	}
	return nil
}

func runMigrationMaintenance(database *sql.DB) error {
	for _, statement := range []string{
		`UPDATE console_sessions SET status = 'closed', error = 'gateway restarted', closed_at = COALESCE(closed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('connecting', 'connected')`,
		`UPDATE command_requests SET status = 'error', error = 'gateway restarted while command was running', completed_at = COALESCE(completed_at, datetime('now')) WHERE status = 'running'`,
		`UPDATE connector_action_requests SET status = 'error', error = 'gateway restarted while connector action was running', completed_at = COALESCE(completed_at, datetime('now')) WHERE status = 'running'`,
		`UPDATE file_transfers SET status = 'failed', error = 'gateway restarted while file transfer was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`UPDATE file_transfer_batches SET status = 'failed', error = 'gateway restarted while file transfer queue was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`UPDATE history_entries SET status = 'error', error = 'gateway restarted while command was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'command_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'error', error = 'gateway restarted while connector action was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'connector_action_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'failed', error = 'gateway restarted while file transfer was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'file_transfer' AND status IN ('pending', 'pending_approval', 'running', 'paused')`,
	} {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("run migration maintenance: %w", err)
		}
	}
	return nil
}
