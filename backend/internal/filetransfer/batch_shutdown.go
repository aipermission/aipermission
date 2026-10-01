package filetransfer

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func (s *Store) FailActive(ctx context.Context, transferError string, batchError string) error {
	now := nowString()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin active file transfer shutdown: %w", err)
	}
	defer tx.Rollback()
	ids, err := transferIDsByStatuses(ctx, tx, StatusPendingApproval, StatusPending, StatusRunning, StatusPaused)
	if err != nil {
		return err
	}
	batchIDs, err := activeShutdownBatchIDs(ctx, tx)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfers
			SET status = ?, error = ?, failure_kind = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE status IN (?, ?)`,
		StatusFailed,
		strings.TrimSpace(transferError),
		FailureKindInterrupted,
		now,
		now,
		StatusPendingApproval,
		StatusPending,
	); err != nil {
		return fmt.Errorf("interrupt undispatched file transfers: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfers
			SET status = ?, error = ?, failure_kind = ?,
				failure_details_json = CASE WHEN remote_staging_ref != ''
					THEN json_patch(failure_details_json, json_object(
						'remote_cleanup_pending', json('true'),
						'recovery_hint', 'Connector-owned staging cleanup will be retried when this workspace opens.'))
					ELSE failure_details_json END,
				completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE status IN (?, ?)`,
		StatusFailed,
		strings.TrimSpace(transferError),
		FailureKindOutcomeUnknown,
		now,
		now,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return fmt.Errorf("mark dispatched file transfers outcome unknown: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfer_batches
			SET status = ?, error = ?, failure_kind = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE status IN (?, ?)`,
		StatusFailed,
		strings.TrimSpace(batchError),
		FailureKindInterrupted,
		now,
		now,
		StatusPendingApproval,
		StatusPending,
	); err != nil {
		return fmt.Errorf("interrupt undispatched file transfer batches: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
			UPDATE file_transfer_batches
			SET status = ?, error = ?, failure_kind = ?, completed_at = COALESCE(completed_at, ?), updated_at = ?
			WHERE status IN (?, ?)`,
		StatusFailed,
		strings.TrimSpace(batchError),
		FailureKindOutcomeUnknown,
		now,
		now,
		StatusRunning,
		StatusPaused,
	); err != nil {
		return fmt.Errorf("mark dispatched file transfer batches outcome unknown: %w", err)
	}
	for _, id := range batchIDs {
		if err := recalculateBatch(ctx, tx, id); err != nil {
			return err
		}
	}
	if err := syncTransferHistoryIDsWithExecutor(ctx, tx, ids); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit active file transfer shutdown: %w", err)
	}
	return nil
}

func activeShutdownBatchIDs(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM file_transfer_batches WHERE status IN (?, ?, ?, ?)
		UNION
		SELECT batch_id FROM file_transfers WHERE batch_id IS NOT NULL AND status IN (?, ?, ?, ?)`,
		StatusPendingApproval, StatusPending, StatusRunning, StatusPaused,
		StatusPendingApproval, StatusPending, StatusRunning, StatusPaused)
	if err != nil {
		return nil, fmt.Errorf("read active shutdown batch ids: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan active shutdown batch id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active shutdown batch ids: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close active shutdown batch ids: %w", err)
	}
	return ids, nil
}
