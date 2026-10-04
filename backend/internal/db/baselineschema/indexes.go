package baselineschema

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
	`CREATE INDEX IF NOT EXISTS idx_bulk_command_idempotency_expires ON bulk_command_idempotency(expires_at);`,
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
