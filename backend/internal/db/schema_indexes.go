package db

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
