package filetransfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/history"
)

func (s *Store) syncTransferHistory(ctx context.Context, id int64) error {
	return syncTransferHistoryWithExecutor(ctx, s.db, id)
}

func syncTransferHistoryWithExecutor(ctx context.Context, executor history.CommandProjectionExecutor, id int64) error {
	if id < 1 {
		return nil
	}
	return history.SyncFileTransferWithExecutor(ctx, executor, id)
}

// SyncHistory repairs the derived history projection after a canonical transfer update.
func (s *Store) SyncHistory(ctx context.Context, id int64) error {
	return s.syncTransferHistory(ctx, id)
}

func syncBatchTransferHistoryWithExecutor(ctx context.Context, executor history.CommandProjectionExecutor, batchID int64) error {
	if batchID < 1 {
		return nil
	}
	rows, err := executor.QueryContext(ctx, `SELECT id FROM file_transfers WHERE batch_id = ?`, batchID)
	if err != nil {
		return fmt.Errorf("read batch transfer ids for history sync: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scan batch transfer id for history sync: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate batch transfer ids for history sync: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close batch transfer ids for history sync: %w", err)
	}
	for _, id := range ids {
		if err := history.SyncFileTransferWithExecutor(ctx, executor, id); err != nil {
			return err
		}
	}
	return nil
}

func syncTransferHistoryIDsWithExecutor(ctx context.Context, executor history.CommandProjectionExecutor, ids []int64) error {
	for _, id := range ids {
		if err := syncTransferHistoryWithExecutor(ctx, executor, id); err != nil {
			return err
		}
	}
	return nil
}

func transferIDsByStatuses(ctx context.Context, executor history.CommandProjectionExecutor, statuses ...string) ([]int64, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, status)
	}
	rows, err := executor.QueryContext(ctx, `
		SELECT id
		FROM file_transfers
		WHERE status IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("read transfer ids for history sync: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan transfer id for history sync: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate transfer ids for history sync: %w", err)
	}
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("close transfer ids for history sync: %w", err)
	}
	return ids, nil
}
