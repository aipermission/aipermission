package history

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	SourceCommandRequest         = "command_request"
	SourceConnectorActionRequest = "connector_action_request"
	SourceFileTransfer           = "file_transfer"
	SourceVaultActionRequest     = "vault_action_request"
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
	return SyncCommandRequestWithExecutor(ctx, s.db, id)
}

type CommandProjectionExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// SyncCommandRequestWithExecutor keeps the canonical command and its history
// projection inside the caller's transaction.
func SyncCommandRequestWithExecutor(ctx context.Context, executor CommandProjectionExecutor, id int64) error {
	if executor == nil {
		return fmt.Errorf("history executor is not configured")
	}
	_, err := executor.ExecContext(ctx, `
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, token_id, runtime_id,
			project_id, target_id, profile_id, target_name, profile_label, source, status, action_name,
			title, summary, input_text, output_text, error, exit_code, approval_required,
			user_note, created_at, started_at, completed_at, updated_at
		)
		SELECT
			?, cr.id, COALESCE(rs.connector_kind, ''), 'command', cr.token_id, cr.runtime_id,
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
	return syncCommandVaultContextWithExecutor(ctx, executor, id)
}

func (s *Store) SyncVaultActionRequest(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history store is not configured")
	}
	return SyncVaultActionRequestWithExecutor(ctx, s.db, id)
}

type projectionExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// SyncVaultActionRequestWithExecutor lets request state and its history
// projection commit in the same database transaction.
func SyncVaultActionRequestWithExecutor(ctx context.Context, executor projectionExecutor, id int64) error {
	if executor == nil {
		return fmt.Errorf("history executor is not configured")
	}
	_, err := executor.ExecContext(ctx, `
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, token_id, runtime_id,
			project_id, target_id, profile_id, target_name, profile_label, source, status,
			action_name, title, summary, preview_json, input_json, output_json, error,
			approval_required, user_note, created_at, started_at, completed_at, updated_at
		)
		SELECT
			?, r.id, COALESCE(rs.connector_kind, 'vault'), 'vault', r.token_id, r.runtime_id,
			r.project_id, ct.id, cp.id, COALESCE(ct.name, p.name), COALESCE(cp.label, ''),
			r.source,
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
		LEFT JOIN connector_targets ct ON ct.id = cp.target_id AND ct.connector_kind = cp.connector_kind
		WHERE r.id = ?
		ON CONFLICT(source_ref_type, source_ref_id) DO UPDATE SET
			token_id = excluded.token_id,
			runtime_id = excluded.runtime_id,
			project_id = excluded.project_id,
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
			output_json = excluded.output_json,
			error = excluded.error,
			approval_required = excluded.approval_required,
			user_note = excluded.user_note,
			started_at = excluded.started_at,
			completed_at = excluded.completed_at,
			updated_at = excluded.updated_at`,
		SourceVaultActionRequest,
		id,
	)
	if err != nil {
		return fmt.Errorf("sync Vault action history entry: %w", err)
	}
	return nil
}

func syncCommandVaultContextWithExecutor(ctx context.Context, executor CommandProjectionExecutor, commandRequestID int64) error {
	var sessionID, generation int64
	var environmentHash, approvalHash string
	err := executor.QueryRowContext(ctx, `
		SELECT cs.id, cs.generation, cs.environment_content_hash, cs.approval_context_hash
		FROM command_requests cr
		JOIN console_sessions cs ON cs.id = cr.session_id
		WHERE cr.id = ? AND cs.environment_content_hash != ''`,
		commandRequestID,
	).Scan(&sessionID, &generation, &environmentHash, &approvalHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read command Vault context: %w", err)
	}
	rows, err := executor.QueryContext(ctx, `
		SELECT vsi.vault_item_id, vsi.vault_item_name, vsi.source_project_id,
		       COALESCE(vsi.binding_id, 0), vsi.binding_revision
		FROM vault_session_items vsi
		WHERE vsi.session_id = ?
		ORDER BY vsi.vault_item_name COLLATE NOCASE, vsi.vault_item_id`,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("read command Vault items: %w", err)
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var itemID, sourceProjectID, bindingID, bindingRevision int64
		var name string
		if err := rows.Scan(&itemID, &name, &sourceProjectID, &bindingID, &bindingRevision); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"item_id": itemID, "name": name, "source_project_id": sourceProjectID,
			"binding_id": bindingID, "binding_revision": bindingRevision,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	payload, err := json.Marshal(map[string]any{
		"vault_session": map[string]any{
			"session_id": sessionID, "session_generation": generation,
			"environment_content_hash": environmentHash,
			"approval_context_hash":    approvalHash,
			"items":                    items,
		},
	})
	if err != nil {
		return fmt.Errorf("encode command Vault context: %w", err)
	}
	if _, err := executor.ExecContext(ctx, `
		UPDATE history_entries SET preview_json = ?
		WHERE source_ref_type = ? AND source_ref_id = ?`,
		string(payload), SourceCommandRequest, commandRequestID,
	); err != nil {
		return fmt.Errorf("persist command Vault context: %w", err)
	}
	return nil
}

func (s *Store) SyncConnectorActionRequest(ctx context.Context, id int64) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("history store is not configured")
	}
	return SyncConnectorActionRequestWithExecutor(ctx, s.db, id)
}

// SyncConnectorActionRequestWithExecutor lets canonical connector request
// state and its history projection commit in one database transaction.
func SyncConnectorActionRequestWithExecutor(ctx context.Context, executor projectionExecutor, id int64) error {
	if executor == nil {
		return fmt.Errorf("history executor is not configured")
	}
	_, err := executor.ExecContext(ctx, `
		INSERT INTO history_entries (
			source_ref_type, source_ref_id, connector_kind, activity_type, token_id, project_id, target_id,
			profile_id, target_name, profile_label, source, status, action_name, title, summary,
			preview_json, input_json, output_text, output_json, error, approval_required, created_at,
			completed_at, updated_at
		)
		SELECT
			?, r.id, r.connector_kind, 'action', r.token_id, t.project_id, r.target_id,
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
			source_ref_type, source_ref_id, connector_kind, activity_type, runtime_id, project_id, target_id,
			profile_id, target_name, profile_label, source, status, action_name, title, summary, preview_json,
			input_text, input_json, output_text, error, progress_current, progress_total,
			bytes_done, bytes_total, approval_required, created_at, started_at, completed_at,
			updated_at
		)
		SELECT
			?, ft.id, COALESCE(rs.connector_kind, ''), 'file_transfer', ft.runtime_id, ct.project_id, ct.id, cp.id,
			COALESCE(ct.name, ''), COALESCE(cp.label, ''), ft.source, ft.status, ft.direction,
			ft.direction || ': ' || ft.file_name,
			ft.remote_path,
			json_object('failure_kind', ft.failure_kind),
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
			preview_json = excluded.preview_json,
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
