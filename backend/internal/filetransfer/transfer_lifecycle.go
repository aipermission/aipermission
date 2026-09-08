package filetransfer

import (
	"context"
	"fmt"
	"strings"
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
	failureKind = normalizeFailureKind(failureKind)
	now := nowString()
	return s.updateTransferWithHistory(ctx, id, "fail file transfer", `
		UPDATE file_transfers
		SET status = ?, error = ?, failure_kind = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(errorText),
		failureKind,
		now,
		now,
		id,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
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
