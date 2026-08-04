package filetransfer

import (
	"context"
	"fmt"
	"strings"
)

func (s *Store) MarkRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
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
	if err != nil {
		return false, fmt.Errorf("mark file transfer running: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read file transfer running rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
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
	_, err := s.db.ExecContext(ctx, `
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
	if err != nil {
		return fmt.Errorf("update file transfer progress: %w", err)
	}
	return s.syncTransferHistory(ctx, id)
}

func (s *Store) Complete(ctx context.Context, id int64, transferred int64, checksum string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, transferred_bytes = CASE WHEN ? >= 0 THEN ? ELSE transferred_bytes END,
			checksum_sha256 = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
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
	if err != nil {
		return false, fmt.Errorf("complete file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read completed file transfer rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) Fail(ctx context.Context, id int64, errorText string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("fail file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read failed file transfer rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) Cancel(ctx context.Context, id int64, errorText string) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
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
	if err != nil {
		return false, fmt.Errorf("cancel file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read canceled file transfer rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) Pause(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	)
	if err != nil {
		return false, fmt.Errorf("pause file transfer: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read paused file transfer rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}
