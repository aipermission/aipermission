package db

import (
	"database/sql"
	"errors"
	"strings"
)

const (
	connectorNativeBaselineVersion     = 1
	connectorNativeBaselineDescription = "0.2 connector-native baseline"
)

var ErrUnsupportedSchema = errors.New("unsupported database schema")

const unsupportedPre02DatabaseMessage = "database uses an unsupported pre-0.2 or non-baseline schema; create a fresh 0.2 database or migrate with the one-time import tool. To migrate a 0.1.x database, run `docker compose --profile migrate up -d --build migration`, then open http://localhost:3211."

func UnsupportedSchemaMessage(err error) string {
	if !errors.Is(err, ErrUnsupportedSchema) {
		return ""
	}
	if message := unsupportedSchemaMessageInChain(err); message != "" {
		return message
	}
	return "database uses an unsupported schema"
}

func unsupportedSchemaMessageInChain(err error) string {
	if err == nil {
		return ""
	}
	prefix := ErrUnsupportedSchema.Error() + ": "
	if strings.HasPrefix(err.Error(), prefix) {
		return strings.TrimPrefix(err.Error(), prefix)
	}
	type multiUnwrapper interface{ Unwrap() []error }
	if joined, ok := err.(multiUnwrapper); ok {
		for _, nested := range joined.Unwrap() {
			if message := unsupportedSchemaMessageInChain(nested); message != "" {
				return message
			}
		}
		return ""
	}
	return unsupportedSchemaMessageInChain(errors.Unwrap(err))
}

type migration struct {
	version     int
	description string
	preflight   func(*sql.Tx) error
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

var backupProviderTableStatements = []string{
	`CREATE TABLE IF NOT EXISTS backup_providers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider_type TEXT NOT NULL,
		name TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled', 'archived')),
		public_json TEXT NOT NULL DEFAULT '{}',
		encrypted_secret_json TEXT NOT NULL DEFAULT '',
		last_checked_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(provider_type, name)
	);`,
	`CREATE TABLE IF NOT EXISTS backup_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		provider_id INTEGER NOT NULL,
		database_id TEXT NOT NULL,
		database_name TEXT NOT NULL,
		provider_file_id TEXT NOT NULL,
		filename TEXT NOT NULL,
		source_machine TEXT NOT NULL DEFAULT '',
		size_bytes INTEGER NOT NULL DEFAULT 0,
		checksum_sha256 TEXT NOT NULL DEFAULT '',
		backup_created_at TEXT NOT NULL,
		uploaded_at TEXT NOT NULL,
		metadata_json TEXT NOT NULL DEFAULT '{}',
		deleted_at TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(provider_id, provider_file_id),
		FOREIGN KEY(provider_id) REFERENCES backup_providers(id) ON DELETE CASCADE
	);`,
}

var projectVaultTableStatements = []string{
	`CREATE TABLE vault_items (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		encrypted_value TEXT NOT NULL DEFAULT '',
		owner_project_id INTEGER NOT NULL,
		secret_type TEXT NOT NULL,
		value_mode TEXT NOT NULL DEFAULT 'text' CHECK (value_mode IN ('text')),
		value_version INTEGER NOT NULL DEFAULT 1,
		metadata_revision INTEGER NOT NULL DEFAULT 1,
		encryption_version INTEGER NOT NULL DEFAULT 1,
		provider TEXT NOT NULL DEFAULT '',
		environment TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		expires_at TEXT,
		expiry_warning_days INTEGER NOT NULL DEFAULT 14,
		last_value_replaced_at TEXT NOT NULL,
		last_used_at TEXT,
		usage_count INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL CHECK (source IN ('imported', 'generated')),
		generator_kind TEXT NOT NULL DEFAULT '',
		generator_parameters_json TEXT NOT NULL DEFAULT '{}',
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(owner_project_id) REFERENCES projects(id) ON DELETE RESTRICT
	);`,
	`CREATE UNIQUE INDEX idx_vault_items_active_name
		ON vault_items(name COLLATE NOCASE)
		WHERE status = 'active';`,
	`CREATE INDEX idx_vault_items_owner_status_name
		ON vault_items(owner_project_id, status, name COLLATE NOCASE);`,
	`CREATE INDEX idx_vault_items_expiry
		ON vault_items(status, expires_at);`,
	`CREATE TABLE vault_item_projects (
		vault_item_id INTEGER NOT NULL,
		project_id INTEGER NOT NULL,
		assignment_revision INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(vault_item_id, project_id),
		FOREIGN KEY(vault_item_id) REFERENCES vault_items(id) ON DELETE CASCADE,
		FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE RESTRICT
	);`,
	`CREATE INDEX idx_vault_item_projects_project
		ON vault_item_projects(project_id, vault_item_id);`,
	`CREATE TABLE vault_item_usage_notes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vault_item_id INTEGER NOT NULL,
		location TEXT NOT NULL,
		notes TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		FOREIGN KEY(vault_item_id) REFERENCES vault_items(id) ON DELETE CASCADE
	);`,
	`CREATE INDEX idx_vault_item_usage_notes_item
		ON vault_item_usage_notes(vault_item_id, id);`,
	`CREATE TABLE vault_item_tags (
		vault_item_id INTEGER NOT NULL,
		tag TEXT NOT NULL COLLATE NOCASE,
		created_at TEXT NOT NULL,
		PRIMARY KEY(vault_item_id, tag),
		FOREIGN KEY(vault_item_id) REFERENCES vault_items(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE vault_default_bindings (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vault_item_id INTEGER NOT NULL,
		source_project_id INTEGER NOT NULL,
		target_id INTEGER NOT NULL,
		profile_id INTEGER NOT NULL,
		replace_existing INTEGER NOT NULL DEFAULT 0 CHECK (replace_existing IN (0, 1)),
		binding_revision INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(vault_item_id, source_project_id, target_id, profile_id),
		FOREIGN KEY(vault_item_id) REFERENCES vault_items(id) ON DELETE CASCADE,
		FOREIGN KEY(source_project_id) REFERENCES projects(id) ON DELETE RESTRICT,
		FOREIGN KEY(target_id) REFERENCES connector_targets(id) ON DELETE RESTRICT,
		FOREIGN KEY(profile_id, target_id) REFERENCES connector_credential_profiles(id, target_id) ON DELETE RESTRICT
	);`,
	`CREATE INDEX idx_vault_default_bindings_profile
		ON vault_default_bindings(target_id, profile_id, vault_item_id);`,
	`CREATE TABLE token_project_capabilities (
		token_id INTEGER NOT NULL,
		project_id INTEGER NOT NULL,
		capability_name TEXT NOT NULL,
		execution_rule TEXT NOT NULL CHECK (execution_rule IN ('always_run', 'approval_required', 'blocked')),
		expires_at TEXT,
		revision INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(token_id, project_id, capability_name),
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE token_project_capability_revisions (
		token_id INTEGER NOT NULL,
		project_id INTEGER NOT NULL,
		capability_name TEXT NOT NULL,
		revision INTEGER NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY(token_id, project_id, capability_name),
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
	);`,
	`CREATE TABLE vault_action_requests (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER NOT NULL,
		project_id INTEGER NOT NULL,
		runtime_id INTEGER,
		action_name TEXT NOT NULL,
		source TEXT NOT NULL DEFAULT 'mcp',
		input_json TEXT NOT NULL DEFAULT '{}',
		reason TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL,
		approval_context_json TEXT NOT NULL DEFAULT '{}',
		approval_context_hash TEXT NOT NULL DEFAULT '',
		idempotency_key TEXT NOT NULL,
		error TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		completed_at TEXT,
		updated_at TEXT NOT NULL,
		UNIQUE(token_id, idempotency_key),
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE RESTRICT,
		FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE SET NULL
	);`,
	`CREATE INDEX idx_vault_action_requests_token_status_created
		ON vault_action_requests(token_id, status, created_at);`,
	`CREATE TABLE vault_session_leases (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		token_id INTEGER NOT NULL,
		runtime_id INTEGER NOT NULL,
		session_id INTEGER NOT NULL,
		session_generation INTEGER NOT NULL,
		approval_context_hash TEXT NOT NULL,
		status TEXT NOT NULL CHECK (status IN ('active', 'expired', 'revoked')),
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE(token_id, runtime_id, session_id, session_generation, approval_context_hash),
		FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
		FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE CASCADE,
		FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE CASCADE
	);`,
	`CREATE INDEX idx_vault_session_leases_active
		ON vault_session_leases(token_id, runtime_id, status, expires_at);`,
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
		project_id, target_id, profile_id, target_name, profile_label, source, status, action_name,
		title, summary, input_text, output_text, error, exit_code, approval_required,
		user_note, created_at, started_at, completed_at, updated_at
	)
	SELECT
		'command_request', cr.id, COALESCE(rs.connector_kind, ''), 'command', cr.token_id, cr.runtime_id,
		ct.project_id, ct.id, cp.id, COALESCE(ct.name, ''), COALESCE(cp.label, ''), cr.source, cr.status, 'exec',
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
		source_ref_type, source_ref_id, connector_kind, activity_type, token_id, project_id, target_id,
		profile_id, target_name, profile_label, source, status, action_name, title, summary,
		preview_json, input_json, output_text, output_json, error, approval_required, created_at,
		completed_at, updated_at
	)
	SELECT
		'connector_action_request', r.id, r.connector_kind, 'action', r.token_id, t.project_id, r.target_id,
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
		source_ref_type, source_ref_id, connector_kind, activity_type, token_id, runtime_id,
		project_id, target_id, profile_id, target_name, profile_label, source, status,
		action_name, title, summary, preview_json, input_json, output_json, error,
		approval_required, user_note, created_at, started_at, completed_at, updated_at
	)
	SELECT
		'vault_action_request', r.id, COALESCE(rs.connector_kind, 'vault'), 'vault',
		r.token_id, r.runtime_id, r.project_id, ct.id, cp.id,
		COALESCE(ct.name, p.name), COALESCE(cp.label, ''), r.source,
		CASE WHEN r.status = 'approval_pending' THEN 'pending_approval' ELSE r.status END,
		r.action_name, r.action_name, r.reason, r.approval_context_json, r.input_json,
		r.output_json, r.error,
		CASE WHEN r.status = 'approval_pending' THEN 1 ELSE 0 END,
		r.user_note, r.created_at,
		CASE WHEN r.status = 'running' OR r.completed_at IS NOT NULL THEN r.updated_at ELSE NULL END,
		r.completed_at, r.updated_at
	FROM vault_action_requests r
	JOIN projects p ON p.id = r.project_id
	LEFT JOIN connector_runtime_surfaces rs ON rs.id = r.runtime_id
	LEFT JOIN connector_credential_profiles cp
		ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
	LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind;`,
	`INSERT OR IGNORE INTO history_entries (
		source_ref_type, source_ref_id, connector_kind, activity_type, runtime_id, project_id, target_id,
		profile_id, target_name, profile_label, source, status, action_name, title, summary,
		input_text, input_json, output_text, error, progress_current, progress_total,
		bytes_done, bytes_total, approval_required, created_at, started_at, completed_at,
		updated_at
	)
	SELECT
		'file_transfer', ft.id, COALESCE(rs.connector_kind, ''), 'file_transfer', ft.runtime_id, ct.project_id, ct.id, cp.id,
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

const auditCommandRequestCreatedTrigger = `CREATE TRIGGER audit_command_request_created
	 AFTER INSERT ON command_requests
	 BEGIN
		INSERT INTO audit_outbox (
			event_id, event_version, actor_type, token_id, project_id, runtime_id,
			connector_kind, target_id, profile_id, action, lifecycle_phase,
			payload_json, occurred_at, created_at
		)
		SELECT lower(hex(randomblob(16))), 1, 'gateway', NEW.token_id, ct.project_id, NEW.runtime_id,
			rs.connector_kind, rs.target_id, rs.profile_id, 'console.command.' || NEW.status,
			NEW.status, printf(
				'{"request_id":%d,"runtime_id":%d,"source":"%s","status":"%s"}',
				NEW.id, NEW.runtime_id, NEW.source, NEW.status
			), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		FROM connector_runtime_surfaces rs
		JOIN connector_targets ct ON ct.id = rs.target_id
		WHERE rs.id = NEW.runtime_id;
	 END;`

const auditCommandRequestStatusChangedTrigger = `CREATE TRIGGER audit_command_request_status_changed
	 AFTER UPDATE OF status ON command_requests
	 WHEN OLD.status <> NEW.status
	 BEGIN
		INSERT INTO audit_outbox (
			event_id, event_version, actor_type, token_id, project_id, runtime_id,
			connector_kind, target_id, profile_id, action, lifecycle_phase,
			payload_json, occurred_at, created_at
		)
		SELECT lower(hex(randomblob(16))), 1, 'gateway', NEW.token_id, ct.project_id, NEW.runtime_id,
			rs.connector_kind, rs.target_id, rs.profile_id, 'console.command.' || NEW.status,
			NEW.status, printf(
				'{"request_id":%d,"runtime_id":%d,"source":"%s","previous_status":"%s","status":"%s"}',
				NEW.id, NEW.runtime_id, NEW.source, OLD.status, NEW.status
			), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		FROM connector_runtime_surfaces rs
		JOIN connector_targets ct ON ct.id = rs.target_id
		WHERE rs.id = NEW.runtime_id;
	 END;`

var migrations = []migration{
	{
		version:     connectorNativeBaselineVersion,
		description: connectorNativeBaselineDescription,
		statements: sqlStatements(
			coreTableStatements,
			fileTransferTableStatements,
			historyTableStatements,
			indexStatements,
			searchIndexStatements,
		),
	},
	{
		version:     2,
		description: "backup provider metadata",
		statements: sqlStatements(
			backupProviderTableStatements,
			[]string{
				`CREATE INDEX IF NOT EXISTS idx_backup_providers_type_status ON backup_providers(provider_type, status);`,
				`CREATE INDEX IF NOT EXISTS idx_backup_records_provider_database_time ON backup_records(provider_id, database_name, backup_created_at);`,
				`CREATE INDEX IF NOT EXISTS idx_backup_records_database_time ON backup_records(database_name, backup_created_at);`,
			},
		),
	},
	{
		version:     3,
		description: "docker live console runtime surfaces",
		statements: []string{
			`INSERT INTO connector_runtime_surfaces (
				connector_kind, target_id, profile_id, capability_kind, label, status, created_at, updated_at
			)
			SELECT p.connector_kind, p.target_id, p.id, 'live_console', p.label, 'active', datetime('now'), datetime('now')
			FROM connector_credential_profiles p
			JOIN connector_targets t ON t.id = p.target_id AND t.connector_kind = p.connector_kind
			WHERE p.connector_kind = 'docker' AND p.status = 'active' AND t.status = 'active'
			ON CONFLICT(connector_kind, target_id, profile_id, capability_kind) DO UPDATE SET
				label = excluded.label,
				status = 'active',
				updated_at = excluded.updated_at`,
		},
	},
	{
		version:     4,
		description: "projects and token project scopes",
		statements: []string{
			`CREATE TABLE projects (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				slug TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);`,
			`CREATE UNIQUE INDEX idx_projects_active_name ON projects(name COLLATE NOCASE) WHERE status = 'active';`,
			`CREATE UNIQUE INDEX idx_projects_slug ON projects(slug);`,
			`INSERT INTO projects (name, slug, status, created_at, updated_at)
			 VALUES ('Ungrouped', 'ungrouped', 'active', datetime('now'), datetime('now'));`,
			`ALTER TABLE connector_targets ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE RESTRICT;`,
			`UPDATE connector_targets SET project_id = (SELECT id FROM projects WHERE slug = 'ungrouped' AND status = 'active');`,
			`DROP INDEX IF EXISTS idx_connector_targets_active_kind_name;`,
			`CREATE UNIQUE INDEX idx_connector_targets_active_project_kind_name ON connector_targets(project_id, connector_kind, name) WHERE status = 'active';`,
			`CREATE INDEX idx_connector_targets_project_status ON connector_targets(project_id, status, name);`,
			`CREATE TRIGGER connector_targets_project_required_insert
			 BEFORE INSERT ON connector_targets
			 WHEN NEW.project_id IS NULL
			 BEGIN
				SELECT RAISE(ABORT, 'connector target project_id is required');
			 END;`,
			`CREATE TRIGGER connector_targets_project_required_update
			 BEFORE UPDATE OF project_id ON connector_targets
			 WHEN NEW.project_id IS NULL
			 BEGIN
				SELECT RAISE(ABORT, 'connector target project_id is required');
			 END;`,
			`CREATE TABLE token_project_scopes (
				token_id INTEGER NOT NULL,
				project_id INTEGER NOT NULL,
				enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL,
				PRIMARY KEY(token_id, project_id),
				FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE CASCADE,
				FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE
			);`,
			`INSERT INTO token_project_scopes (token_id, project_id, enabled, created_at, updated_at)
			 SELECT tok.id, p.id, 1, datetime('now'), datetime('now')
			 FROM api_tokens tok CROSS JOIN projects p WHERE p.status = 'active';`,
			`CREATE INDEX idx_token_project_scopes_enabled ON token_project_scopes(token_id, enabled, project_id);`,
			`ALTER TABLE history_entries ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL;`,
			`UPDATE history_entries
			 SET project_id = COALESCE(
				(SELECT ct.project_id FROM connector_targets ct WHERE ct.id = history_entries.target_id),
				(SELECT ct.project_id
				 FROM connector_runtime_surfaces rs
				 JOIN connector_targets ct ON ct.id = rs.target_id
				 WHERE rs.id = history_entries.runtime_id)
			 );`,
			`CREATE INDEX idx_history_entries_project_created ON history_entries(project_id, created_at);`,
			`ALTER TABLE audit_logs ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE SET NULL;`,
			`UPDATE audit_logs
			 SET project_id = COALESCE(
				(SELECT ct.project_id FROM connector_targets ct WHERE ct.id = audit_logs.target_id),
				(SELECT ct.project_id
				 FROM connector_runtime_surfaces rs
				 JOIN connector_targets ct ON ct.id = rs.target_id
				 WHERE rs.id = audit_logs.runtime_id)
			 );`,
			`CREATE INDEX idx_audit_logs_project_created ON audit_logs(project_id, created_at);`,
		},
	},
	{
		version:     5,
		description: "project vault foundation",
		statements:  projectVaultTableStatements,
	},
	{
		version:     6,
		description: "principal-aware exact console sessions",
		statements: []string{
			`ALTER TABLE console_sessions ADD COLUMN generation INTEGER NOT NULL DEFAULT 0;`,
			`ALTER TABLE console_sessions ADD COLUMN principal_kind TEXT NOT NULL DEFAULT 'local_operator';`,
			`ALTER TABLE console_sessions ADD COLUMN principal_token_id INTEGER;`,
			`ALTER TABLE console_sessions ADD COLUMN workspace_id TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE console_sessions ADD COLUMN runtime_instance_id TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE console_sessions ADD COLUMN environment_content_hash TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE console_sessions ADD COLUMN approval_context_hash TEXT NOT NULL DEFAULT '';`,
			`UPDATE console_sessions SET generation = id WHERE generation = 0;`,
			`CREATE UNIQUE INDEX idx_console_sessions_runtime_generation ON console_sessions(runtime_id, generation);`,
			`ALTER TABLE connector_action_requests ADD COLUMN session_id INTEGER;`,
			`ALTER TABLE connector_action_requests ADD COLUMN session_generation INTEGER;`,
			`CREATE INDEX idx_connector_action_requests_session ON connector_action_requests(session_id, session_generation);`,
		},
	},
	{
		version:     7,
		description: "Vault approvals and session enforcement",
		statements: []string{
			`ALTER TABLE vault_action_requests ADD COLUMN output_json TEXT NOT NULL DEFAULT 'null';`,
			`ALTER TABLE vault_action_requests ADD COLUMN user_note TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE vault_action_requests ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';`,
			`UPDATE vault_action_requests
			 SET expires_at = strftime('%Y-%m-%dT%H:%M:%fZ', created_at, '+15 minutes')
			 WHERE expires_at = '';`,
			`CREATE INDEX idx_vault_action_requests_pending_expiry
				ON vault_action_requests(status, expires_at);`,
			`ALTER TABLE vault_session_leases ADD COLUMN project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE;`,
			`ALTER TABLE vault_session_leases ADD COLUMN environment_content_hash TEXT NOT NULL DEFAULT '';`,
			`UPDATE vault_session_leases
			 SET environment_content_hash = COALESCE(
				(SELECT environment_content_hash
				 FROM console_sessions
				 WHERE console_sessions.id = vault_session_leases.session_id),
				''
			 );`,
			`CREATE INDEX idx_vault_session_leases_project
				ON vault_session_leases(project_id, status, expires_at);`,
			`CREATE INDEX idx_vault_action_requests_project_status
				ON vault_action_requests(project_id, status, id);`,
			`CREATE INDEX idx_vault_action_requests_runtime_status
				ON vault_action_requests(runtime_id, status, id);`,
			`CREATE INDEX idx_vault_action_requests_action_status
				ON vault_action_requests(action_name, status, id);`,
			`CREATE INDEX idx_vault_session_leases_session_status
				ON vault_session_leases(session_id, session_generation, status);`,
			`CREATE TABLE vault_session_items (
				session_id INTEGER NOT NULL,
				vault_item_id INTEGER NOT NULL,
				source_project_id INTEGER NOT NULL,
				value_version INTEGER NOT NULL,
				metadata_revision INTEGER NOT NULL,
				binding_id INTEGER,
				binding_revision INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				PRIMARY KEY(session_id, vault_item_id),
				FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE CASCADE,
				FOREIGN KEY(vault_item_id) REFERENCES vault_items(id) ON DELETE CASCADE,
				FOREIGN KEY(source_project_id) REFERENCES projects(id) ON DELETE RESTRICT,
				FOREIGN KEY(binding_id) REFERENCES vault_default_bindings(id) ON DELETE SET NULL
			);`,
			`CREATE INDEX idx_vault_session_items_item
				ON vault_session_items(vault_item_id, session_id);`,
			`CREATE INDEX idx_vault_session_items_binding
				ON vault_session_items(binding_id, session_id);`,
		},
	},
	{
		version:     8,
		description: "globally unique active Vault item names",
		// Fresh databases already receive the global index from the baseline.
		// This upgrades unpublished development databases that applied the
		// earlier owner-scoped index before the branch changed.
		preflight: requireGloballyUniqueVaultItemNames,
		statements: []string{
			`DROP INDEX IF EXISTS idx_vault_items_active_owner_name;`,
			`CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_items_active_name
				ON vault_items(name COLLATE NOCASE)
				WHERE status = 'active';`,
		},
	},
	{
		version:     9,
		description: "immutable Vault session item snapshots",
		statements: []string{
			`CREATE TABLE vault_session_items_snapshot (
				session_id INTEGER NOT NULL,
				vault_item_id INTEGER NOT NULL,
				vault_item_name TEXT NOT NULL,
				source_project_id INTEGER NOT NULL,
				value_version INTEGER NOT NULL,
				metadata_revision INTEGER NOT NULL,
				binding_id INTEGER,
				binding_revision INTEGER NOT NULL DEFAULT 0,
				created_at TEXT NOT NULL,
				PRIMARY KEY(session_id, vault_item_id),
				FOREIGN KEY(session_id) REFERENCES console_sessions(id) ON DELETE CASCADE,
				FOREIGN KEY(source_project_id) REFERENCES projects(id) ON DELETE RESTRICT,
				FOREIGN KEY(binding_id) REFERENCES vault_default_bindings(id) ON DELETE SET NULL
			);`,
			`INSERT INTO vault_session_items_snapshot (
				session_id, vault_item_id, vault_item_name, source_project_id,
				value_version, metadata_revision, binding_id, binding_revision, created_at
			)
			SELECT vsi.session_id, vsi.vault_item_id, vi.name, vsi.source_project_id,
			       vsi.value_version, vsi.metadata_revision, vsi.binding_id,
			       vsi.binding_revision, vsi.created_at
			FROM vault_session_items vsi
			JOIN vault_items vi ON vi.id = vsi.vault_item_id;`,
			`DROP TABLE vault_session_items;`,
			`ALTER TABLE vault_session_items_snapshot RENAME TO vault_session_items;`,
			`CREATE INDEX idx_vault_session_items_item
				ON vault_session_items(vault_item_id, session_id);`,
			`CREATE INDEX idx_vault_session_items_binding
				ON vault_session_items(binding_id, session_id);`,
		},
	},
	{
		version:     10,
		description: "self-hosted encrypted backup provider",
		statements: []string{
			`UPDATE backup_providers
			 SET status = 'archived', encrypted_secret_json = '', updated_at = datetime('now')
			 WHERE provider_type = 'google_drive'`,
		},
	},
	{
		version:     11,
		description: "connector action idempotency",
		statements: []string{
			`ALTER TABLE connector_action_requests ADD COLUMN idempotency_key TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE connector_action_requests ADD COLUMN idempotency_identity_hash TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE connector_action_requests ADD COLUMN idempotency_scope TEXT NOT NULL DEFAULT '';`,
			`CREATE UNIQUE INDEX idx_connector_action_requests_idempotency
				ON connector_action_requests(idempotency_scope, idempotency_key)
				WHERE idempotency_key <> '';`,
		},
	},
	{
		version:     12,
		description: "transactional audit outbox",
		statements: []string{
			`ALTER TABLE audit_logs ADD COLUMN event_id TEXT;`,
			`CREATE UNIQUE INDEX idx_audit_logs_event_id
				ON audit_logs(event_id)
				WHERE event_id IS NOT NULL;`,
			`CREATE TABLE audit_outbox (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				event_id TEXT NOT NULL UNIQUE,
				event_version INTEGER NOT NULL,
				actor_type TEXT NOT NULL,
				token_id INTEGER,
				project_id INTEGER,
				runtime_id INTEGER,
				connector_kind TEXT NOT NULL DEFAULT '',
				target_id INTEGER,
				profile_id INTEGER,
				action_request_id INTEGER,
				action TEXT NOT NULL,
				lifecycle_phase TEXT NOT NULL DEFAULT '',
				payload_json TEXT NOT NULL DEFAULT '{}',
				occurred_at TEXT NOT NULL,
				created_at TEXT NOT NULL,
				delivered_at TEXT,
				attempt_count INTEGER NOT NULL DEFAULT 0,
				last_error TEXT NOT NULL DEFAULT '',
				last_attempt_at TEXT,
				FOREIGN KEY(token_id) REFERENCES api_tokens(id) ON DELETE SET NULL,
				FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE SET NULL,
				FOREIGN KEY(runtime_id) REFERENCES connector_runtime_surfaces(id) ON DELETE SET NULL
			);`,
			`CREATE INDEX idx_audit_outbox_pending
				ON audit_outbox(delivered_at, id);`,
			`CREATE INDEX idx_audit_outbox_attempts
				ON audit_outbox(attempt_count, last_attempt_at)
				WHERE delivered_at IS NULL;`,
			`CREATE TABLE audit_dispatch_state (
				id INTEGER PRIMARY KEY CHECK (id = 1),
				failure_count INTEGER NOT NULL DEFAULT 0,
				last_error TEXT NOT NULL DEFAULT '',
				last_failure_at TEXT,
				last_success_at TEXT,
				updated_at TEXT NOT NULL
			);`,
			`INSERT INTO audit_dispatch_state (id, updated_at)
				VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'));`,
		},
	},
	{
		version:     13,
		description: "transactional runtime lifecycle audit transitions",
		statements: []string{
			`CREATE TRIGGER audit_console_session_created
			 AFTER INSERT ON console_sessions
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'console.session.created', 'created', printf(
						'{"session_id":%d,"runtime_id":%d,"generation":%d,"status":"%s"}',
						NEW.id, NEW.runtime_id, NEW.generation, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_console_session_status_changed
			 AFTER UPDATE OF status ON console_sessions
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'console.session.' || NEW.status, NEW.status, printf(
						'{"session_id":%d,"runtime_id":%d,"generation":%d,"previous_status":"%s","status":"%s"}',
						NEW.id, NEW.runtime_id, NEW.generation, OLD.status, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_created
			 AFTER INSERT ON file_transfers
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id, 'file_transfer.created',
					NEW.status, printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","status":"%s"}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, NEW.direction, NEW.source, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_status_changed
			 AFTER UPDATE OF status ON file_transfers
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.' || NEW.status, NEW.status, printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s"}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, NEW.direction,
						NEW.source, OLD.status, NEW.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_queue_changed
			 AFTER UPDATE OF queue_index ON file_transfers
			 WHEN OLD.queue_index <> NEW.queue_index
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.queue_updated', 'updated', printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"previous_queue_index":%d,"queue_index":%d}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, OLD.queue_index, NEW.queue_index
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_removed
			 AFTER DELETE ON file_transfers
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, OLD.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.removed', 'deleted', printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"status":"%s"}',
						OLD.id, COALESCE(OLD.batch_id, 0), OLD.runtime_id, OLD.status
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = OLD.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_batch_created
			 AFTER INSERT ON file_transfer_batches
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.created', NEW.status, printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","status":"%s","total_items":%d}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source, NEW.status, NEW.total_items
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_batch_status_changed
			 AFTER UPDATE OF status ON file_transfer_batches
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.' || NEW.status, NEW.status, printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s","total_items":%d}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source, OLD.status, NEW.status, NEW.total_items
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`CREATE TRIGGER audit_file_transfer_batch_archive_ready
			 AFTER UPDATE OF archive_path ON file_transfer_batches
			 WHEN OLD.archive_path <> NEW.archive_path AND NEW.archive_path <> ''
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.archive_ready', 'completed', printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s"}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		},
	},
	{
		version:     14,
		description: "audit delivery recovery and command lifecycle projection",
		statements: []string{
			`ALTER TABLE audit_logs ADD COLUMN event_version INTEGER NOT NULL DEFAULT 1;`,
			`ALTER TABLE audit_logs ADD COLUMN lifecycle_phase TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE audit_outbox ADD COLUMN next_attempt_at TEXT;`,
			`ALTER TABLE audit_outbox ADD COLUMN dead_lettered_at TEXT;`,
			`DROP INDEX idx_audit_outbox_pending;`,
			`CREATE INDEX idx_audit_outbox_pending
				ON audit_outbox(dead_lettered_at, delivered_at, next_attempt_at, id);`,
			auditCommandRequestCreatedTrigger,
			auditCommandRequestStatusChangedTrigger,
		},
	},
	{
		version:     15,
		description: "repair Vault session lease environment binding",
		preflight:   ensureVaultSessionLeaseEnvironmentHash,
	},
	{
		version:     16,
		description: "repair audit delivery recovery schema",
		preflight:   ensureAuditRecoverySchema,
		statements: []string{
			`DROP INDEX IF EXISTS idx_audit_outbox_pending;`,
			`CREATE INDEX idx_audit_outbox_pending
				ON audit_outbox(dead_lettered_at, delivered_at, next_attempt_at, id);`,
			`DROP TRIGGER IF EXISTS audit_command_request_created;`,
			`DROP TRIGGER IF EXISTS audit_command_request_status_changed;`,
			auditCommandRequestCreatedTrigger,
			auditCommandRequestStatusChangedTrigger,
		},
	},
	{
		version:     17,
		description: "structured file transfer failure outcomes",
		statements: []string{
			`ALTER TABLE file_transfers ADD COLUMN failure_kind TEXT NOT NULL DEFAULT '';`,
			`ALTER TABLE file_transfer_batches ADD COLUMN failure_kind TEXT NOT NULL DEFAULT '';`,
			`DROP TRIGGER IF EXISTS audit_file_transfer_status_changed;`,
			`CREATE TRIGGER audit_file_transfer_status_changed
			 AFTER UPDATE OF status ON file_transfers
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.' || NEW.status, NEW.status, printf(
						'{"transfer_id":%d,"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s","failure_kind":"%s"}',
						NEW.id, COALESCE(NEW.batch_id, 0), NEW.runtime_id, NEW.direction,
						NEW.source, OLD.status, NEW.status, NEW.failure_kind
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
			`DROP TRIGGER IF EXISTS audit_file_transfer_batch_status_changed;`,
			`CREATE TRIGGER audit_file_transfer_batch_status_changed
			 AFTER UPDATE OF status ON file_transfer_batches
			 WHEN OLD.status <> NEW.status
			 BEGIN
				INSERT INTO audit_outbox (
					event_id, event_version, actor_type, project_id, runtime_id,
					connector_kind, target_id, profile_id, action, lifecycle_phase,
					payload_json, occurred_at, created_at
				)
				SELECT lower(hex(randomblob(16))), 1, 'gateway', ct.project_id, NEW.runtime_id,
					rs.connector_kind, rs.target_id, rs.profile_id,
					'file_transfer.batch.' || NEW.status, NEW.status, printf(
						'{"batch_id":%d,"runtime_id":%d,"direction":"%s","source":"%s","previous_status":"%s","status":"%s","failure_kind":"%s","total_items":%d}',
						NEW.id, NEW.runtime_id, NEW.direction, NEW.source, OLD.status,
						NEW.status, NEW.failure_kind, NEW.total_items
					), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
				FROM connector_runtime_surfaces rs
				JOIN connector_targets ct ON ct.id = rs.target_id
				WHERE rs.id = NEW.runtime_id;
			 END;`,
		},
	},
	retentionIndexMigration,
	recordEnvelopeBoundaryMigration,
	s3UploadProjectionScrubMigration,
	recordEnvelopeWriteGuardMigration,
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
