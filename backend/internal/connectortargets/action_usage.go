package connectortargets

import (
	"context"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	maxTokenActionRequestRows     = 20_000
	maxTokenActionRequestBytes    = 256 << 20
	maxTokenRunningActionRequests = 4
	// Connector results permit 4 MiB of structured output and 1 MiB each for
	// display and error text before terminal persistence.
	actionTerminalReservationBytes = 6 << 20
)

type actionRequestCapacity struct {
	rows    int64
	bytes   int64
	running int64
}

type ActionRequestUsage struct {
	Rows  int64
	Bytes int64
}

func (s *Store) ActionRequestUsageForToken(ctx context.Context, tokenID int64) (ActionRequestUsage, error) {
	if s == nil || s.db == nil {
		return ActionRequestUsage{}, fmt.Errorf("connector target store is not configured")
	}
	usage, _, err := actionRequestUsageForToken(ctx, s.db, tokenID, 0)
	return usage, err
}

func actionRequestWithinTokenCapacity(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64) (bool, error) {
	return actionRequestWithinCapacity(ctx, executor, tokenID, incomingID, actionRequestCapacity{
		rows: maxTokenActionRequestRows, bytes: maxTokenActionRequestBytes, running: maxTokenRunningActionRequests,
	})
}

func actionRequestWithinCapacity(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64, capacity actionRequestCapacity) (bool, error) {
	usage, running, err := actionRequestUsageForToken(ctx, executor, tokenID, incomingID)
	if err != nil {
		return false, err
	}
	return usage.Rows <= capacity.rows && usage.Bytes <= capacity.bytes && running <= capacity.running, nil
}

func actionRequestUsageForToken(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64) (ActionRequestUsage, int64, error) {
	var usage ActionRequestUsage
	var running int64
	err := executor.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(
			LENGTH(CAST(title AS BLOB)) + LENGTH(CAST(summary AS BLOB)) +
			LENGTH(CAST(preview_json AS BLOB)) + LENGTH(CAST(source AS BLOB)) +
			LENGTH(CAST(input_json AS BLOB)) + LENGTH(CAST(encrypted_payload_json AS BLOB)) +
			LENGTH(CAST(reason AS BLOB)) + LENGTH(CAST(status AS BLOB)) +
			LENGTH(CAST(output_json AS BLOB)) + LENGTH(CAST(display_text AS BLOB)) +
			LENGTH(CAST(error AS BLOB)) + LENGTH(CAST(approval_context AS BLOB)) +
			LENGTH(CAST(approval_context_hash AS BLOB)) + LENGTH(CAST(approval_context_drift AS BLOB)) +
			LENGTH(CAST(retry_policy_json AS BLOB)) + LENGTH(CAST(idempotency_key AS BLOB)) +
			LENGTH(CAST(idempotency_identity_hash AS BLOB)) + LENGTH(CAST(idempotency_scope AS BLOB)) +
			LENGTH(CAST(execution_owner AS BLOB)) + LENGTH(CAST(execution_lease_expires_at AS BLOB)) +
			LENGTH(CAST(dispatch_started_at AS BLOB)) + LENGTH(CAST(created_at AS BLOB)) +
			LENGTH(CAST(COALESCE(completed_at, '') AS BLOB)) +
			CASE WHEN id = ? OR status IN ('running', 'approval_pending') THEN ? ELSE 0 END
		), 0), COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0)
		FROM connector_action_requests
		WHERE token_id = ?`, incomingID, actionTerminalReservationBytes, tokenID).Scan(&usage.Rows, &usage.Bytes, &running)
	return usage, running, err
}
