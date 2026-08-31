package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/history"
)

func (s *Store) PauseBatch(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	)
	if err != nil {
		return false, fmt.Errorf("pause file transfer batch: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE batch_id = ? AND status = ?`,
		StatusPaused,
		now,
		id,
		StatusRunning,
	); err != nil {
		return false, fmt.Errorf("pause file transfer batch items: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read paused file transfer batch rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) ResumeBatch(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, updated_at = ?
		WHERE id = ? AND status = ?`,
		StatusRunning,
		now,
		id,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("resume file transfer batch: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, updated_at = ?
		WHERE batch_id = ? AND status = ?`,
		StatusRunning,
		now,
		id,
		StatusPaused,
	); err != nil {
		return false, fmt.Errorf("resume file transfer batch items: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read resumed file transfer batch rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) UpdatePausedBatchQueue(ctx context.Context, id int64, orderedPendingIDs []int64) ([]Record, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin file transfer batch queue update: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM file_transfer_batches WHERE id = ?`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("read file transfer batch status: %w", err)
	}
	if status != StatusPaused {
		return nil, ErrInvalidState
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, queue_index, status, temp_path
		FROM file_transfers
		WHERE batch_id = ?
		ORDER BY queue_index ASC, id ASC`,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("read paused file transfer batch items: %w", err)
	}
	type queueItem struct {
		id         int64
		queueIndex int
		status     string
		tempPath   string
	}
	var items []queueItem
	for rows.Next() {
		var item queueItem
		if err := rows.Scan(&item.id, &item.queueIndex, &item.status, &item.tempPath); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan paused file transfer batch item: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate paused file transfer batch items: %w", err)
	}
	rows.Close()

	pending := map[int64]queueItem{}
	maxStableIndex := -1
	for _, item := range items {
		if item.status == StatusPending {
			pending[item.id] = item
			continue
		}
		if item.queueIndex > maxStableIndex {
			maxStableIndex = item.queueIndex
		}
	}
	seen := map[int64]bool{}
	for _, itemID := range orderedPendingIDs {
		if itemID < 1 {
			return nil, ErrInvalidArgument
		}
		if seen[itemID] {
			return nil, ErrInvalidArgument
		}
		if _, ok := pending[itemID]; !ok {
			return nil, ErrInvalidState
		}
		seen[itemID] = true
	}

	now := nowString()
	var removed []Record
	for itemID, item := range pending {
		if seen[itemID] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			DELETE FROM file_transfers
			WHERE id = ? AND status = ?`,
			itemID,
			StatusPending,
		); err != nil {
			return nil, fmt.Errorf("remove paused file transfer batch item: %w", err)
		}
		removed = append(removed, Record{ID: itemID, TempPath: item.tempPath})
	}

	for index, itemID := range orderedPendingIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfers
			SET queue_index = ?, updated_at = ?
			WHERE id = ? AND status = ?`,
			maxStableIndex+1+index,
			now,
			itemID,
			StatusPending,
		); err != nil {
			return nil, fmt.Errorf("reorder paused file transfer batch item: %w", err)
		}
	}

	if err := recalculateBatch(ctx, tx, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit file transfer batch queue update: %w", err)
	}
	for _, item := range removed {
		if err := history.NewStore(s.db).DeleteSourceRef(ctx, history.SourceFileTransfer, item.ID); err != nil {
			return nil, err
		}
	}
	if err := s.syncBatchTransferHistory(ctx, id); err != nil {
		return nil, err
	}
	return removed, nil
}

func (s *Store) CancelBatch(ctx context.Context, id int64, errorText string) (bool, error) {
	return s.finishBatch(ctx, id, StatusCanceled, errorText)
}

func (s *Store) FailBatch(ctx context.Context, id int64, errorText string) (bool, error) {
	return s.finishBatch(ctx, id, StatusFailed, errorText)
}

func (s *Store) finishBatch(ctx context.Context, id int64, status string, errorText string) (bool, error) {
	if status != StatusCanceled && status != StatusFailed {
		return false, fmt.Errorf("finish file transfer batch: unsupported terminal status %q", status)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin file transfer batch finalization: %w", err)
	}
	defer tx.Rollback()

	now := nowString()
	result, err := tx.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?, ?)`,
		status,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPending,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("finalize file transfer batch: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read finalized file transfer batch rows: %w", err)
	}
	if rows == 0 {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE batch_id = ? AND status IN (?, ?, ?, ?)`,
		status,
		strings.TrimSpace(errorText),
		now,
		now,
		id,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return false, fmt.Errorf("finalize file transfer batch items: %w", err)
	}
	if err := recalculateBatch(ctx, tx, id); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit file transfer batch finalization: %w", err)
	}
	if err := s.syncBatchTransferHistory(ctx, id); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) FailActive(ctx context.Context, transferError string, batchError string) error {
	now := nowString()
	ids, err := s.transferIDsByStatuses(ctx, StatusPendingApproval, StatusPending, StatusRunning, StatusPaused)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfers
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE status IN (?, ?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(transferError),
		now,
		now,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return fmt.Errorf("fail active file transfers: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
		WHERE status IN (?, ?, ?, ?)`,
		StatusFailed,
		strings.TrimSpace(batchError),
		now,
		now,
		StatusPendingApproval,
		StatusPending,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return fmt.Errorf("fail active file transfer batches: %w", err)
	}
	return s.syncTransferHistoryIDs(ctx, ids)
}
func (s *Store) RecalculateBatch(ctx context.Context, id int64) error {
	return recalculateBatch(ctx, s.db, id)
}

type batchRecalculator interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func recalculateBatch(ctx context.Context, execer batchRecalculator, id int64) error {
	now := nowString()
	_, err := execer.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET
			total_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ?),
			completed_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ? AND status = ?),
			failed_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ? AND status = ?),
			canceled_items = (SELECT COUNT(*) FROM file_transfers WHERE batch_id = ? AND status = ?),
			size_bytes = COALESCE((SELECT SUM(size_bytes) FROM file_transfers WHERE batch_id = ?), 0),
			transferred_bytes = COALESCE((SELECT SUM(transferred_bytes) FROM file_transfers WHERE batch_id = ?), 0),
			bytes_per_second = COALESCE((SELECT SUM(bytes_per_second) FROM file_transfers WHERE batch_id = ? AND status IN (?, ?)), 0),
			eta_seconds = CASE
				WHEN COALESCE((SELECT SUM(bytes_per_second) FROM file_transfers WHERE batch_id = ? AND status IN (?, ?)), 0) > 0
				THEN CAST((
					COALESCE((SELECT SUM(size_bytes) FROM file_transfers WHERE batch_id = ?), 0) -
					COALESCE((SELECT SUM(transferred_bytes) FROM file_transfers WHERE batch_id = ?), 0)
				) / COALESCE((SELECT SUM(bytes_per_second) FROM file_transfers WHERE batch_id = ? AND status IN (?, ?)), 1) AS INTEGER)
				ELSE -1
			END,
			updated_at = ?
		WHERE id = ?`,
		id,
		id, StatusCompleted,
		id, StatusFailed,
		id, StatusCanceled,
		id,
		id,
		id, StatusRunning, StatusPaused,
		id, StatusRunning, StatusPaused,
		id,
		id,
		id, StatusRunning, StatusPaused,
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("recalculate file transfer batch: %w", err)
	}
	return nil
}

func (s *Store) CompleteBatch(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = CASE
				WHEN failed_items > 0 THEN ?
				WHEN completed_items > 0 THEN ?
				WHEN canceled_items > 0 THEN ?
				ELSE ?
			END,
			completed_at = COALESCE(completed_at, ?),
			bytes_per_second = 0,
			eta_seconds = 0,
			updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusFailed,
		StatusCompleted,
		StatusCanceled,
		StatusCompleted,
		now,
		now,
		id,
		StatusRunning,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("complete file transfer batch: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read completed file transfer batch rows: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) SetBatchArchive(ctx context.Context, id int64, archivePath string) error {
	now := nowString()
	_, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET archive_path = ?, updated_at = ?
		WHERE id = ?`,
		strings.TrimSpace(archivePath),
		now,
		id,
	)
	if err != nil {
		return fmt.Errorf("set file transfer batch archive: %w", err)
	}
	return nil
}
