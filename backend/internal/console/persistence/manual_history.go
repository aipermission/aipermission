package persistence

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/history"
)

func CloseStaleManualRunningRows(ctx context.Context, database *sql.DB, sessionID, beforeID int64, reason, now string) error {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin manual command history transaction: %w", err)
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `
		SELECT id FROM command_requests
		WHERE source = 'manual' AND session_id = ? AND status = 'running'
			AND (? = 0 OR id < ?)`, sessionID, beforeID, beforeID)
	if err != nil {
		return fmt.Errorf("list stale manual command rows: %w", err)
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan stale manual command row: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate stale manual command rows: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close stale manual command rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE command_requests
		SET status = 'untracked', tracking_reason = ?, completed_at = COALESCE(completed_at, ?)
		WHERE source = 'manual' AND session_id = ? AND status = 'running'
			AND (? = 0 OR id < ?)`, reason, now, sessionID, beforeID, beforeID); err != nil {
		return err
	}
	for _, id := range ids {
		if err := history.SyncCommandRequestWithExecutor(ctx, tx, id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit manual command history transaction: %w", err)
	}
	return nil
}
