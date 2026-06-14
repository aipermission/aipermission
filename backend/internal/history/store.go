package history

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	SourceCommandRequest         = "command_request"
	SourceConnectorActionRequest = "connector_action_request"
	SourceFileTransfer           = "file_transfer"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) SyncCommandRequest(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history store is not configured")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, token_id, runtime_id,
			target_id, profile_id, target_name, profile_label, source, status, action_name,
			title, summary, input_text, output_text, error, exit_code, approval_required,
			user_note, created_at, started_at, completed_at, updated_at
		)
		SELECT
			?, cr.id, COALESCE(rs.connector_kind, ''), 'command', cr.token_id, cr.runtime_id,
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
			COALESCE(cr.completed_at, datetime('now'))
		FROM command_requests cr
		LEFT JOIN connector_runtime_surfaces rs ON rs.id = cr.runtime_id
		LEFT JOIN connector_credential_profiles cp ON cp.id = rs.profile_id AND cp.target_id = rs.target_id AND cp.connector_kind = rs.connector_kind
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE cr.id = ?
		ON CONFLICT(source_ref_type, source_ref_id) DO UPDATE SET
			token_id = excluded.token_id,
			runtime_id = excluded.runtime_id,
			target_id = excluded.target_id,
			profile_id = excluded.profile_id,
			target_name = excluded.target_name,
			profile_label = excluded.profile_label,
			source = excluded.source,
			status = excluded.status,
			title = excluded.title,
			summary = excluded.summary,
			input_text = excluded.input_text,
			output_text = excluded.output_text,
			error = excluded.error,
			exit_code = excluded.exit_code,
			approval_required = excluded.approval_required,
			user_note = excluded.user_note,
			completed_at = excluded.completed_at,
			updated_at = excluded.updated_at`,
		SourceCommandRequest,
		id,
	)
	if err != nil {
		return fmt.Errorf("sync command history entry: %w", err)
	}
	return nil
}

func (s *Store) SyncConnectorActionRequest(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history store is not configured")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, token_id, target_id,
			profile_id, target_name, profile_label, source, status, action_name, title, summary,
			preview_json, input_json, output_text, output_json, error, approval_required, created_at,
			completed_at, updated_at
		)
		SELECT
			?, r.id, r.connector_kind, 'action', r.token_id, r.target_id,
			r.profile_id, t.name, p.label, COALESCE(NULLIF(r.source, ''), 'mcp'),
			CASE WHEN r.status = 'approval_pending' THEN 'pending_approval' ELSE r.status END,
			r.action_name, COALESCE(NULLIF(r.title, ''), r.action_name),
			COALESCE(NULLIF(r.summary, ''), r.reason), r.preview_json,
			r.input_json, r.display_text, r.output_json, r.error,
			CASE WHEN r.status = 'approval_pending' THEN 1 ELSE 0 END,
			r.created_at, r.completed_at, COALESCE(r.completed_at, datetime('now'))
		FROM connector_action_requests r
		JOIN connector_targets t ON t.id = r.target_id
		JOIN connector_credential_profiles p ON p.id = r.profile_id AND p.target_id = r.target_id AND p.connector_kind = r.connector_kind
		WHERE r.id = ?
		ON CONFLICT(source_ref_type, source_ref_id) DO UPDATE SET
			token_id = excluded.token_id,
			target_id = excluded.target_id,
			profile_id = excluded.profile_id,
			target_name = excluded.target_name,
			profile_label = excluded.profile_label,
			status = excluded.status,
			action_name = excluded.action_name,
			title = excluded.title,
			summary = excluded.summary,
			preview_json = excluded.preview_json,
			input_json = excluded.input_json,
			output_text = excluded.output_text,
			output_json = excluded.output_json,
			error = excluded.error,
			approval_required = excluded.approval_required,
			completed_at = excluded.completed_at,
			updated_at = excluded.updated_at`,
		SourceConnectorActionRequest,
		id,
	)
	if err != nil {
		return fmt.Errorf("sync connector action history entry: %w", err)
	}
	return nil
}

func (s *Store) SyncFileTransfer(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history store is not configured")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, runtime_id, target_id,
			profile_id, target_name, profile_label, source, status, action_name, title, summary,
			input_text, input_json, output_text, error, progress_current, progress_total,
			bytes_done, bytes_total, approval_required, created_at, started_at, completed_at,
			updated_at
		)
		SELECT
			?, ft.id, COALESCE(rs.connector_kind, ''), 'file_transfer', ft.runtime_id, ct.id, cp.id,
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
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE ft.id = ?
		ON CONFLICT(source_ref_type, source_ref_id) DO UPDATE SET
			runtime_id = excluded.runtime_id,
			target_id = excluded.target_id,
			profile_id = excluded.profile_id,
			target_name = excluded.target_name,
			profile_label = excluded.profile_label,
			source = excluded.source,
			status = excluded.status,
			action_name = excluded.action_name,
			title = excluded.title,
			summary = excluded.summary,
			input_text = excluded.input_text,
			output_text = excluded.output_text,
			error = excluded.error,
			progress_current = excluded.progress_current,
			progress_total = excluded.progress_total,
			bytes_done = excluded.bytes_done,
			bytes_total = excluded.bytes_total,
			approval_required = excluded.approval_required,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			updated_at = excluded.updated_at`,
		SourceFileTransfer,
		id,
	)
	if err != nil {
		return fmt.Errorf("sync file transfer history entry: %w", err)
	}
	return nil
}

func (s *Store) DeleteSourceRef(ctx context.Context, sourceRefType string, sourceRefID int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history store is not configured")
	}
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM history_entries
		WHERE source_ref_type = ? AND source_ref_id = ?`,
		sourceRefType,
		sourceRefID,
	)
	if err != nil {
		return fmt.Errorf("delete history entry: %w", err)
	}
	return nil
}
