package filetransfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) MarkBatchRunning(ctx context.Context, id int64) (bool, error) {
	now := nowString()
	result, err := s.db.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, started_at = COALESCE(started_at, ?), updated_at = ?
		WHERE id = ? AND status IN (?, ?)`,
		StatusRunning,
		now,
		now,
		id,
		StatusPending,
		StatusPaused,
	)
	if err != nil {
		return false, fmt.Errorf("mark file transfer batch running: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read file transfer batch running rows: %w", err)
	}
	if rows > 0 {
		if err := s.syncBatchTransferHistory(ctx, id); err != nil {
			return false, err
		}
	}
	return rows > 0, nil
}

func (s *Store) ApproveBatch(ctx context.Context, id int64, request BatchApprovalRequest) (BatchRecord, []Record, error) {
	approvedIDs, err := normalizeApprovedItemIDs(request.ApprovedItemIDs)
	if err != nil {
		return BatchRecord{}, nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BatchRecord{}, nil, fmt.Errorf("begin file transfer batch approval: %w", err)
	}
	defer tx.Rollback()

	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM file_transfer_batches WHERE id = ?`, id).Scan(&status); errors.Is(err, sql.ErrNoRows) {
		return BatchRecord{}, nil, ErrNotFound
	} else if err != nil {
		return BatchRecord{}, nil, fmt.Errorf("read file transfer batch approval status: %w", err)
	}
	if status != StatusPendingApproval {
		return BatchRecord{}, nil, ErrInvalidState
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, temp_path
		FROM file_transfers
		WHERE batch_id = ? AND status = ?
		ORDER BY queue_index ASC, id ASC`,
		id,
		StatusPendingApproval,
	)
	if err != nil {
		return BatchRecord{}, nil, fmt.Errorf("read pending approval file transfer items: %w", err)
	}
	type pendingItem struct {
		id       int64
		tempPath string
	}
	var pending []pendingItem
	for rows.Next() {
		var item pendingItem
		if err := rows.Scan(&item.id, &item.tempPath); err != nil {
			rows.Close()
			return BatchRecord{}, nil, fmt.Errorf("scan pending approval file transfer item: %w", err)
		}
		pending = append(pending, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BatchRecord{}, nil, fmt.Errorf("iterate pending approval file transfer items: %w", err)
	}
	rows.Close()
	if len(pending) == 0 {
		return BatchRecord{}, nil, ErrInvalidState
	}
	approvedSet := map[int64]bool{}
	for _, id := range approvedIDs {
		approvedSet[id] = true
	}
	foundApproved := map[int64]bool{}
	for _, item := range pending {
		if approvedSet[item.id] {
			foundApproved[item.id] = true
		}
	}
	for id := range approvedSet {
		if !foundApproved[id] {
			return BatchRecord{}, nil, ErrInvalidArgument
		}
	}

	note := strings.TrimSpace(request.Note)
	now := nowString()
	var rejected []Record
	for _, item := range pending {
		if approvedSet[item.id] {
			if _, err := tx.ExecContext(ctx, `
				UPDATE file_transfers
				SET status = ?, updated_at = ?
				WHERE id = ? AND status = ?`,
				StatusPending,
				now,
				item.id,
				StatusPendingApproval,
			); err != nil {
				return BatchRecord{}, nil, fmt.Errorf("approve file transfer item: %w", err)
			}
			continue
		}
		reason := note
		if reason == "" {
			reason = "rejected by local user"
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfers
			SET status = ?, error = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE id = ? AND status = ?`,
			StatusCanceled,
			reason,
			now,
			now,
			item.id,
			StatusPendingApproval,
		); err != nil {
			return BatchRecord{}, nil, fmt.Errorf("reject file transfer item: %w", err)
		}
		rejected = append(rejected, Record{ID: item.id, TempPath: item.tempPath})
	}

	nextStatus := StatusPending
	if len(approvedIDs) == 0 {
		nextStatus = StatusCanceled
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE file_transfer_batches
		SET status = ?, approval_note = ?, error = CASE WHEN ? = ? THEN ? ELSE error END,
			completed_at = CASE WHEN ? = ? THEN COALESCE(completed_at, ?) ELSE completed_at END,
			updated_at = ?
		WHERE id = ? AND status = ?`,
		nextStatus,
		note,
		nextStatus,
		StatusCanceled,
		rejectionNote(note),
		nextStatus,
		StatusCanceled,
		now,
		now,
		id,
		StatusPendingApproval,
	); err != nil {
		return BatchRecord{}, nil, fmt.Errorf("approve file transfer batch: %w", err)
	}
	if err := recalculateBatch(ctx, tx, id); err != nil {
		return BatchRecord{}, nil, err
	}
	if err := tx.Commit(); err != nil {
		return BatchRecord{}, nil, fmt.Errorf("commit file transfer batch approval: %w", err)
	}
	batch, err := s.GetBatch(ctx, id)
	if err != nil {
		return BatchRecord{}, nil, err
	}
	if err := s.syncBatchTransferHistory(ctx, id); err != nil {
		return BatchRecord{}, nil, err
	}
	return batch, rejected, nil
}

func (s *Store) DeclineBatch(ctx context.Context, id int64, note string) (BatchRecord, []Record, error) {
	batch, rejected, err := s.ApproveBatch(ctx, id, BatchApprovalRequest{ApprovedItemIDs: nil, Note: note})
	if err != nil {
		return BatchRecord{}, nil, err
	}
	return batch, rejected, nil
}
