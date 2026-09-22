package connectortargets

import (
	"context"
	"fmt"
	"strings"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/history"
)

// StalePendingActionRequestsForToken permanently invalidates approvals and
// claimed-but-not-dispatched requests prepared under an older token
// authorization state. When the store is transaction-backed, the canonical
// request and history projection remain in the caller's transaction.
func (s *Store) StalePendingActionRequestsForToken(ctx context.Context, tokenID int64, reason string) ([]int64, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("connector target store is not configured")
	}
	if tokenID < 1 {
		return nil, ValidationError("token_id must be positive")
	}

	executor, commit, rollback, err := s.transaction(ctx, "stale connector action requests for token")
	if err != nil {
		return nil, err
	}
	defer rollback()

	rows, err := executor.QueryContext(ctx, `
		SELECT id
		FROM connector_action_requests
		WHERE token_id = ? AND (
			status = ? OR
			(status = ? AND trim(COALESCE(dispatch_started_at, '')) = '')
		)
		ORDER BY id`, tokenID, string(connectors.ResultApprovalPending), string(connectors.ResultRunning))
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	now := nowString()
	for _, id := range ids {
		result, err := executor.ExecContext(ctx, `
			UPDATE connector_action_requests
			SET status = ?, error = ?, approval_context_drift = 'authorization', completed_at = ?
			WHERE id = ? AND token_id = ? AND (
				status = ? OR
				(status = ? AND trim(COALESCE(dispatch_started_at, '')) = '')
			)`,
			string(connectors.ResultStale), strings.TrimSpace(reason), now,
			id, tokenID, string(connectors.ResultApprovalPending), string(connectors.ResultRunning),
		)
		if err != nil {
			return nil, err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected != 1 {
			return nil, fmt.Errorf("stale connector action request %d: concurrent lifecycle change", id)
		}
		if err := history.SyncConnectorActionRequestWithExecutor(ctx, executor, id); err != nil {
			return nil, err
		}
	}
	if err := commit(); err != nil {
		return nil, err
	}
	return ids, nil
}
