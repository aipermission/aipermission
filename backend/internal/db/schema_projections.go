package db

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
