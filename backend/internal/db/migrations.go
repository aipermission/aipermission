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
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL
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
		closed_at TEXT
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
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL
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
}

var commonIndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_ssh_keys_name ON ssh_keys(name);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_name ON api_tokens(name);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens(token_hash);`,
	`CREATE INDEX IF NOT EXISTS idx_api_tokens_expires_at ON api_tokens(expires_at);`,
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

var unifiedHistoryStatements = []string{
	`CREATE TABLE IF NOT EXISTS history_entries (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_ref_type TEXT NOT NULL,
		source_ref_id INTEGER NOT NULL,
		connector_kind TEXT NOT NULL,
		activity_type TEXT NOT NULL,
		token_id INTEGER,
		server_id INTEGER,
		target_id INTEGER,
		profile_id INTEGER,
		target_name TEXT NOT NULL DEFAULT '',
		profile_label TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		action_name TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		summary TEXT NOT NULL DEFAULT '',
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
	`CREATE INDEX IF NOT EXISTS idx_history_entries_created ON history_entries(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_kind_created ON history_entries(connector_kind, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_activity_created ON history_entries(activity_type, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_status_created ON history_entries(status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_target_created ON history_entries(target_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_profile_created ON history_entries(profile_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_server_created ON history_entries(server_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entries_source_created ON history_entries(source, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_history_entry_labels_label ON history_entry_labels(label_id);`,
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, token_id, server_id,
		target_id, profile_id, target_name, profile_label, source, status, action_name,
		title, summary, input_text, output_text, error, exit_code, approval_required,
		user_note, created_at, started_at, completed_at, updated_at
	)
	SELECT
		'command_request', cr.id, 'ssh', 'command', cr.token_id, cr.server_id,
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
	LEFT JOIN connector_credential_profiles cp ON cp.id = cr.server_id AND cp.connector_kind = 'ssh'
	LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh';`,
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, token_id, target_id,
		profile_id, target_name, profile_label, source, status, action_name, title, summary,
		input_json, output_text, output_json, error, approval_required, created_at,
		completed_at, updated_at
	)
	SELECT
		'connector_action_request', r.id, r.connector_kind, 'action', r.token_id, r.target_id,
		r.profile_id, t.name, p.label, COALESCE(NULLIF(r.source, ''), 'mcp'),
		CASE WHEN r.status = 'approval_pending' THEN 'pending_approval' ELSE r.status END,
		r.action_name, r.action_name,
		r.reason, r.input_json, r.display_text, r.output_json, r.error,
		CASE WHEN r.status = 'approval_pending' THEN 1 ELSE 0 END,
		r.created_at, r.completed_at, COALESCE(r.completed_at, r.created_at)
	FROM connector_action_requests r
	JOIN connector_targets t ON t.id = r.target_id
	JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id AND p.connector_kind = r.connector_kind;`,
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, server_id, target_id,
		profile_id, target_name, profile_label, source, status, action_name, title, summary,
		input_text, input_json, output_text, error, progress_current, progress_total,
		bytes_done, bytes_total, approval_required, created_at, started_at, completed_at,
		updated_at
	)
	SELECT
		'file_transfer', ft.id, 'ssh', 'file_transfer', ft.server_id, ct.id, cp.id,
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
	LEFT JOIN connector_credential_profiles cp ON cp.id = ft.server_id AND cp.connector_kind = 'ssh'
	LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = 'ssh';`,
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
		updated_at TEXT NOT NULL
	);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_created ON file_transfers(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_server_status_created ON file_transfers(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_direction_created ON file_transfers(direction, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_status_created ON file_transfers(status, created_at);`,
}

var bulkFileTransferStatements = []string{
	`CREATE TABLE IF NOT EXISTS file_transfer_batches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER NOT NULL,
		direction TEXT NOT NULL CHECK (direction IN ('upload', 'download')),
		source TEXT NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'mcp')),
		status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'canceled')),
		archive_name TEXT NOT NULL DEFAULT '',
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
		updated_at TEXT NOT NULL
	);`,
	`CREATE TABLE IF NOT EXISTS file_transfers_next (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_id INTEGER,
		queue_index INTEGER NOT NULL DEFAULT 0,
		server_id INTEGER NOT NULL,
		direction TEXT NOT NULL CHECK (direction IN ('upload', 'download')),
		source TEXT NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'mcp')),
		status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'canceled')),
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
		FOREIGN KEY(batch_id) REFERENCES file_transfer_batches(id) ON DELETE SET NULL
	);`,
	`INSERT INTO file_transfers_next (
		id, server_id, direction, source, status, local_path, remote_path, file_name,
		size_bytes, transferred_bytes, checksum_sha256, temp_path, error, created_at,
		started_at, completed_at, updated_at
	)
		SELECT id, server_id, direction, source, status, local_path, remote_path, file_name,
			size_bytes, transferred_bytes, checksum_sha256, temp_path, error, created_at,
			started_at, completed_at, updated_at
		FROM file_transfers;`,
	`DROP TABLE file_transfers;`,
	`ALTER TABLE file_transfers_next RENAME TO file_transfers;`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_created ON file_transfer_batches(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_server_status_created ON file_transfer_batches(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_created ON file_transfers(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_server_status_created ON file_transfers(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_direction_created ON file_transfers(direction, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_status_created ON file_transfers(status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_batch_queue ON file_transfers(batch_id, queue_index, id);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_batch_status ON file_transfers(batch_id, status);`,
}

var fileTransferApprovalStatements = []string{
	`DROP TABLE IF EXISTS file_transfer_batches_next;`,
	`CREATE TABLE file_transfer_batches_next (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		server_id INTEGER NOT NULL,
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
		updated_at TEXT NOT NULL
	);`,
	`INSERT INTO file_transfer_batches_next (
		id, server_id, direction, source, status, archive_name, overwrite, archive_path, total_items,
		completed_items, failed_items, canceled_items, size_bytes, transferred_bytes,
		bytes_per_second, eta_seconds, error, created_at, started_at, completed_at, updated_at
	)
		SELECT id, server_id, direction, source, status, archive_name, 0, archive_path, total_items,
			completed_items, failed_items, canceled_items, size_bytes, transferred_bytes,
			bytes_per_second, eta_seconds, error, created_at, started_at, completed_at, updated_at
		FROM file_transfer_batches;`,
	`DROP TABLE file_transfer_batches;`,
	`ALTER TABLE file_transfer_batches_next RENAME TO file_transfer_batches;`,
	`DROP TABLE IF EXISTS file_transfers_next;`,
	`CREATE TABLE file_transfers_next (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		batch_id INTEGER,
		queue_index INTEGER NOT NULL DEFAULT 0,
		server_id INTEGER NOT NULL,
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
		FOREIGN KEY(batch_id) REFERENCES file_transfer_batches(id) ON DELETE SET NULL
	);`,
	`INSERT INTO file_transfers_next (
		id, batch_id, queue_index, server_id, direction, source, status, local_path,
		remote_path, file_name, size_bytes, transferred_bytes, bytes_per_second,
		eta_seconds, checksum_sha256, temp_path, error, created_at, started_at,
		completed_at, updated_at
	)
		SELECT id, batch_id, queue_index, server_id, direction, source, status, local_path,
			remote_path, file_name, size_bytes, transferred_bytes, bytes_per_second,
			eta_seconds, checksum_sha256, temp_path, error, created_at, started_at,
			completed_at, updated_at
		FROM file_transfers;`,
	`DROP TABLE file_transfers;`,
	`ALTER TABLE file_transfers_next RENAME TO file_transfers;`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_created ON file_transfer_batches(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfer_batches_server_status_created ON file_transfer_batches(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_created ON file_transfers(created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_server_status_created ON file_transfers(server_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_direction_created ON file_transfers(direction, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_status_created ON file_transfers(status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_batch_queue ON file_transfers(batch_id, queue_index, id);`,
	`CREATE INDEX IF NOT EXISTS idx_file_transfers_batch_status ON file_transfers(batch_id, status);`,
}

var permissionExpirationStatements = []string{}

var approvalContextStatements = []string{
	`ALTER TABLE command_requests ADD COLUMN approval_context TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE command_requests ADD COLUMN approval_context_hash TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE command_requests ADD COLUMN approval_context_drift TEXT NOT NULL DEFAULT '';`,
	`CREATE INDEX IF NOT EXISTS idx_command_requests_approval_context_hash ON command_requests(approval_context_hash);`,
}

var serverSSHAdvancedSettingsStatements = []string{}

var connectorPersistenceTableStatements = []string{
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
	`CREATE TABLE IF NOT EXISTS connector_action_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER,
		target_id INTEGER NOT NULL,
		profile_id INTEGER NOT NULL,
		connector_kind TEXT NOT NULL,
		action_name TEXT NOT NULL,
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
}

var connectorPersistenceIndexStatements = []string{
	`CREATE INDEX IF NOT EXISTS idx_connector_targets_kind_name ON connector_targets(connector_kind, name);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_targets_active_kind_name ON connector_targets(connector_kind, name) WHERE status = 'active';`,
	`CREATE INDEX IF NOT EXISTS idx_connector_credential_profiles_target ON connector_credential_profiles(target_id);`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_credential_profiles_active_label ON connector_credential_profiles(target_id, label) WHERE status = 'active';`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_credential_profiles_ssh_one_active ON connector_credential_profiles(target_id) WHERE connector_kind = 'ssh' AND status = 'active';`,
	`CREATE INDEX IF NOT EXISTS idx_token_connector_action_permissions_token ON token_connector_action_permissions(token_id);`,
	`CREATE INDEX IF NOT EXISTS idx_token_connector_action_permissions_lookup ON token_connector_action_permissions(token_id, target_id, profile_id, action_name);`,
	`CREATE INDEX IF NOT EXISTS idx_token_connector_action_permissions_expires_at ON token_connector_action_permissions(expires_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_token_status_created ON connector_action_requests(token_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_target_status_created ON connector_action_requests(target_id, status, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_kind_action_created ON connector_action_requests(connector_kind, action_name, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_approval_context_hash ON connector_action_requests(approval_context_hash);`,
}

var connectorPersistenceStatements = sqlStatements(connectorPersistenceTableStatements, connectorPersistenceIndexStatements)

var connectorAuditHardeningStatements = []string{
	`ALTER TABLE connector_action_requests ADD COLUMN source TEXT NOT NULL DEFAULT 'mcp';`,
	`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_source_created ON connector_action_requests(source, created_at);`,
	`ALTER TABLE audit_logs ADD COLUMN connector_kind TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE audit_logs ADD COLUMN target_id INTEGER;`,
	`ALTER TABLE audit_logs ADD COLUMN profile_id INTEGER;`,
	`ALTER TABLE audit_logs ADD COLUMN action_request_id INTEGER;`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_connector_created ON audit_logs(connector_kind, target_id, profile_id, created_at);`,
	`CREATE INDEX IF NOT EXISTS idx_audit_logs_action_request ON audit_logs(action_request_id);`,
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
		statements:  historyLabelStatements,
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
	{
		version:     6,
		description: "bulk file transfer queue",
		statements:  bulkFileTransferStatements,
	},
	{
		version:     7,
		description: "file transfer approvals",
		statements:  fileTransferApprovalStatements,
	},
	{
		version:     8,
		description: "token server permission expiration",
		statements:  permissionExpirationStatements,
	},
	{
		version:     9,
		description: "approval context snapshots",
		statements:  approvalContextStatements,
	},
	{
		version:     10,
		description: "server ssh advanced startup settings",
		statements:  serverSSHAdvancedSettingsStatements,
	},
	{
		version:     11,
		description: "connector target and action persistence",
		statements:  connectorPersistenceStatements,
	},
	{
		version:     12,
		description: "connector audit and action source metadata",
		statements:  connectorAuditHardeningStatements,
	},
	{
		version:     13,
		description: "unified history entries",
		statements:  unifiedHistoryStatements,
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
	for _, statement := range sqlStatements(historyLabelStatements, fileTransferStatements, connectorPersistenceTableStatements) {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("ensure current schema: %w", err)
		}
	}
	if err := ensureConnectorLifecycleSchema(database); err != nil {
		return fmt.Errorf("ensure connector lifecycle schema: %w", err)
	}
	for _, statement := range connectorPersistenceIndexStatements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("ensure connector persistence indexes: %w", err)
		}
	}
	if err := ensureConnectorAuditHardeningSchema(database); err != nil {
		return fmt.Errorf("ensure connector audit schema: %w", err)
	}
	for _, statement := range unifiedHistoryStatements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("ensure unified history schema: %w", err)
		}
	}
	return nil
}

func ensureConnectorLifecycleSchema(database *sql.DB) error {
	if err := ensureColumn(database, "connector_credential_profiles", "status", "status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived'))"); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_targets_active_kind_name ON connector_targets(connector_kind, name) WHERE status = 'active';`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_credential_profiles_active_label ON connector_credential_profiles(target_id, label) WHERE status = 'active';`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_connector_credential_profiles_ssh_one_active ON connector_credential_profiles(target_id) WHERE connector_kind = 'ssh' AND status = 'active';`,
	} {
		if _, err := database.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureConnectorAuditHardeningSchema(database *sql.DB) error {
	for _, column := range []struct {
		table      string
		name       string
		definition string
	}{
		{table: "connector_action_requests", name: "source", definition: "source TEXT NOT NULL DEFAULT 'mcp'"},
		{table: "audit_logs", name: "connector_kind", definition: "connector_kind TEXT NOT NULL DEFAULT ''"},
		{table: "audit_logs", name: "target_id", definition: "target_id INTEGER"},
		{table: "audit_logs", name: "profile_id", definition: "profile_id INTEGER"},
		{table: "audit_logs", name: "action_request_id", definition: "action_request_id INTEGER"},
	} {
		if err := ensureColumn(database, column.table, column.name, column.definition); err != nil {
			return err
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_source_created ON connector_action_requests(source, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_connector_created ON audit_logs(connector_kind, target_id, profile_id, created_at);`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action_request ON audit_logs(action_request_id);`,
	} {
		if _, err := database.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(database *sql.DB, table string, column string, definition string) error {
	exists, err := dbColumnExists(database, table, column)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := database.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + definition); err != nil {
		return err
	}
	return nil
}

func dbColumnExists(database *sql.DB, table string, column string) (bool, error) {
	rows, err := database.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	return false, nil
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
		`UPDATE connector_action_requests SET status = 'error', error = 'gateway restarted while connector action was running', completed_at = COALESCE(completed_at, datetime('now')) WHERE status = 'running'`,
		`UPDATE file_transfers SET status = 'failed', error = 'gateway restarted while file transfer was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`UPDATE file_transfer_batches SET status = 'failed', error = 'gateway restarted while file transfer queue was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`UPDATE history_entries SET status = 'error', error = 'gateway restarted while command was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'command_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'error', error = 'gateway restarted while connector action was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'connector_action_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'failed', error = 'gateway restarted while file transfer was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'file_transfer' AND status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`DELETE FROM token_connector_action_permissions
			WHERE action_name = 'upload_files'
				AND EXISTS (
					SELECT 1
					FROM connector_targets t
					WHERE t.id = token_connector_action_permissions.target_id
						AND t.connector_kind = 'ssh'
				)`,
	} {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("run migration maintenance: %w", err)
		}
	}
	return nil
}
