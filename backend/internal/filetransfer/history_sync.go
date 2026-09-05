package filetransfer

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/history"
)

func (s *Store) syncTransferHistory(ctx context.Context, id int64) error {
	if id < 1 {
		return nil
	}
	return history.NewStore(s.db).SyncFileTransfer(ctx, id)
}

// SyncHistory repairs the derived history projection after a canonical transfer update.
func (s *Store) SyncHistory(ctx context.Context, id int64) error {
	return s.syncTransferHistory(ctx, id)
}

func (s *Store) syncBatchTransferHistory(ctx context.Context, batchID int64) error {
	return syncBatchTransferHistoryWithExecutor(ctx, s.db, batchID)
}

func syncBatchTransferHistoryWithExecutor(ctx context.Context, executor history.CommandProjectionExecutor, batchID int64) error {
	if batchID < 1 {
		return nil
	}
	rows, err := executor.QueryContext(ctx, `SELECT id FROM file_transfers WHERE batch_id = ?`, batchID)
	if err != nil {
		return fmt.Errorf("read batch transfer ids for history sync: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan batch transfer id for history sync: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate batch transfer ids for history sync: %w", err)
	}
	for _, id := range ids {
		if err := history.SyncFileTransferWithExecutor(ctx, executor, id); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) syncTransferHistoryIDs(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if err := s.syncTransferHistory(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) transferIDsByStatuses(ctx context.Context, statuses ...string) ([]int64, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, status)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id
		FROM file_transfers
		WHERE status IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("read transfer ids for history sync: %w", err)
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan transfer id for history sync: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate transfer ids for history sync: %w", err)
	}
	return ids, nil
}
