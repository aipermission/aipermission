package filetransfer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func (s *Store) MarkRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	return s.updateTransferWithHistory(ctx, id, "mark file transfer running", `
		UPDATE file_transfers
		SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		StatusRunning,
		now,
		now,
		id,
		StatusPending,
		StatusPendingApproval,
		StatusPaused,
	)
}

func (s *Store) UpdateProgress(ctx context.Context, id int64, transferred int64, size int64) error {
	return s.UpdateProgressStats(ctx, id, transferred, size, 0, -1)
}

func (s *Store) UpdateProgressStats(ctx context.Context, id int64, transferred int64, size int64, bytesPerSecond int64, etaSeconds int64) error {
	if transferred < 0 {
		transferred = 0
	}
	if size < 0 {
		size = 0
	}
	if bytesPerSecond < 0 {
		bytesPerSecond = 0
	}
	now := nowString()
	_, err := s.updateTransferWithHistory(ctx, id, "update file transfer progress", `
		UPDATE file_transfers
		SET transferred_bytes = ?, size_bytes = CASE WHEN ? > 0 THEN ? ELSE size_bytes END,
			bytes_per_second = ?, eta_seconds = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		transferred,
		size,
		size,
		bytesPerSecond,
		etaSeconds,
		now,
		id,
		StatusRunning,
		StatusPaused,
	)
	return err
}

func (s *Store) UpdateEvidence(ctx context.Context, id, transferred int64, checksum string) error {
	if transferred < 0 {
		transferred = 0
	}
	now := nowString()
	_, err := s.updateTransferWithHistory(ctx, id, "update file transfer evidence", `
		UPDATE file_transfers
		SET transferred_bytes = CASE WHEN ? > transferred_bytes THEN ? ELSE transferred_bytes END,
			checksum_sha256 = CASE WHEN ? != '' THEN ? ELSE checksum_sha256 END,
			updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		transferred, transferred, strings.TrimSpace(checksum), strings.TrimSpace(checksum), now, id,
		StatusRunning, StatusPaused,
	)
	return err
}

func (s *Store) Complete(ctx context.Context, id int64, transferred int64, checksum string) (bool, error) {
	now := nowString()
	return s.updateTransferWithHistory(ctx, id, "complete file transfer", `
		UPDATE file_transfers
		SET status = ?, transferred_bytes = CASE WHEN ? >= 0 THEN ? ELSE transferred_bytes END,
			checksum_sha256 = ?, error = '', failure_kind = '',
			completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusCompleted,
		transferred,
		transferred,
		strings.TrimSpace(checksum),
		now,
		now,
		id,
		StatusRunning,
		StatusPaused,
	)
}

func (s *Store) Fail(ctx context.Context, id int64, errorText string) (bool, error) {
	return s.FailWithKind(ctx, id, errorText, FailureKindUnknown)
}

func (s *Store) FailWithKind(ctx context.Context, id int64, errorText string, failureKind string) (bool, error) {
	return s.FailWithDetails(ctx, id, errorText, failureKind, nil)
}

func (s *Store) FailWithDetails(ctx context.Context, id int64, errorText string, failureKind string, details map[string]any) (bool, error) {
	failureKind = normalizeFailureKind(failureKind)
	detailsJSON, err := json.Marshal(details)
	if err != nil || len(detailsJSON) > MaxFailureDetailsJSONBytes {
		return false, ErrInvalidArgument
	}
	if len(detailsJSON) == 0 || string(detailsJSON) == "null" {
		detailsJSON = []byte("{}")
	}
	now := nowString()
	return s.updateTransferWithHistory(ctx, id, "fail file transfer", `
		UPDATE file_transfers
		SET status = ?, error = ?, failure_kind = ?, failure_details_json = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(errorText),
		failureKind,
		string(detailsJSON),
		now,
		now,
		id,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
}

func (s *Store) SetTempExpiry(ctx context.Context, id int64, tempPath string, expiresAt time.Time) error {
	if id < 1 || strings.TrimSpace(tempPath) == "" || expiresAt.IsZero() {
		return ErrInvalidArgument
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers SET temp_expires_at = ?
		WHERE id = ? AND temp_path = ?`, expiresAt.UTC().Format(time.RFC3339Nano), id, tempPath)
	if err != nil {
		return fmt.Errorf("set file transfer temp expiry: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read file transfer temp expiry update: %w", err)
	}
	if rows != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ClearTempPath(ctx context.Context, id int64, tempPath string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers SET temp_path = '', temp_expires_at = NULL
		WHERE id = ? AND temp_path = ?`, id, tempPath)
	if err != nil {
		return fmt.Errorf("clear file transfer temp path: %w", err)
	}
	return nil
}

func (s *Store) SetRemoteStagingRef(ctx context.Context, id int64, ref string) error {
	ref = strings.TrimSpace(ref)
	if id < 1 || ref == "" {
		return ErrInvalidArgument
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers SET remote_staging_ref = ?
		WHERE id = ? AND status = ? AND remote_staging_ref = ''`, ref, id, StatusRunning)
	if err != nil {
		return fmt.Errorf("set remote file transfer staging: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read remote file transfer staging update: %w", err)
	}
	if rows != 1 {
		return ErrInvalidState
	}
	return nil
}

func (s *Store) ClearRemoteStagingRef(ctx context.Context, id int64, ref string) error {
	ref = strings.TrimSpace(ref)
	if id < 1 || ref == "" {
		return ErrInvalidArgument
	}
	_, err := s.updateTransferWithHistory(ctx, id, "clear remote file transfer staging", `
		UPDATE file_transfers
		SET remote_staging_ref = '',
			failure_details_json = json_remove(failure_details_json, '$.remote_cleanup_pending', '$.recovery_hint')
		WHERE id = ? AND remote_staging_ref = ?`, id, ref)
	if err != nil {
		return fmt.Errorf("clear remote file transfer staging: %w", err)
	}
	return nil
}

func (s *Store) ListTempCleanupCandidates(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, status, direction, temp_path, COALESCE(temp_expires_at, ''), updated_at
		FROM file_transfers
		WHERE temp_path != '' AND status IN (?, ?, ?)
		ORDER BY id`, StatusCompleted, StatusFailed, StatusCanceled)
	if err != nil {
		return nil, fmt.Errorf("list file transfer temp cleanup candidates: %w", err)
	}
	defer rows.Close()
	items := []Record{}
	for rows.Next() {
		var item Record
		if err := rows.Scan(&item.ID, &item.Status, &item.Direction, &item.TempPath, &item.TempExpiresAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan file transfer temp cleanup candidate: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate file transfer temp cleanup candidates: %w", err)
	}
	return items, nil
}

func (s *Store) ListRemoteStagingCandidates(ctx context.Context) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, runtime_id, status, remote_staging_ref
		FROM file_transfers
		WHERE remote_staging_ref != ''
		ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list remote file transfer staging candidates: %w", err)
	}
	defer rows.Close()
	items := []Record{}
	for rows.Next() {
		var item Record
		if err := rows.Scan(&item.ID, &item.RuntimeID, &item.Status, &item.RemoteStagingRef); err != nil {
			return nil, fmt.Errorf("scan remote file transfer staging candidate: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate remote file transfer staging candidates: %w", err)
	}
	return items, nil
}

func (s *Store) ListManagedTempPaths(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT temp_path FROM file_transfers WHERE temp_path != ''
		UNION
		SELECT archive_path FROM file_transfer_batches WHERE archive_path != ''`)
	if err != nil {
		return nil, fmt.Errorf("list managed file transfer temp paths: %w", err)
	}
	defer rows.Close()
	paths := map[string]struct{}{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan managed file transfer temp path: %w", err)
		}
		paths[filepath.Clean(path)] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate managed file transfer temp paths: %w", err)
	}
	return paths, nil
}

func (s *Store) Cancel(ctx context.Context, id int64, errorText string) (bool, error) {
	now := nowString()
	return s.updateTransferWithHistory(ctx, id, "cancel file transfer", `
		UPDATE file_transfers
		SET status = ?, error = ?, failure_kind = '', completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?, ?)`,
		StatusCanceled,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
}

func normalizeFailureKind(value string) string {
	switch strings.TrimSpace(value) {
	case FailureKindTimeout, FailureKindValidation, FailureKindLocalPersistence, FailureKindOutcomeUnknown, FailureKindInterrupted:
		return strings.TrimSpace(value)
	default:
		return FailureKindUnknown
	}
}

func (s *Store) Pause(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	return s.updateTransferWithHistory(ctx, id, "pause file transfer", `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	)
}

func (s *Store) updateTransferWithHistory(ctx context.Context, id int64, operation string, query string, args ...any) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin %s: %w", operation, err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("%s: %w", operation, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read %s rows: %w", operation, err)
	}
	if rows == 0 {
		return false, nil
	}
	if err := syncTransferHistoryWithExecutor(ctx, tx, id); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit %s: %w", operation, err)
	}
	return true, nil
}
