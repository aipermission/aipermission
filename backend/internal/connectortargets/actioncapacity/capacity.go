// Package actioncapacity measures stored token action usage in the caller's transaction.
package actioncapacity

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/sqldb"
)

const (
	MaxRows    = 20_000
	MaxBytes   = 256 << 20
	MaxRunning = 4
	// Connector results permit 4 MiB of structured output and 1 MiB each for
	// display and error text before terminal persistence.
	TerminalReservationBytes = 6 << 20
)

type Limits struct {
	Rows    int64
	Bytes   int64
	Running int64
}

type Usage struct {
	Rows    int64
	Bytes   int64
	Running int64
}

func WithinDefault(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64) (bool, error) {
	return Within(ctx, executor, tokenID, incomingID, Limits{
		Rows: MaxRows, Bytes: MaxBytes, Running: MaxRunning,
	})
}

func Within(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64, limits Limits) (bool, error) {
	usage, err := Measure(ctx, executor, tokenID, incomingID)
	if err != nil {
		return false, err
	}
	return usage.Rows <= limits.Rows && usage.Bytes <= limits.Bytes && usage.Running <= limits.Running, nil
}

// Measure requires a configured executor; it neither opens nor commits a transaction.
// Each incoming, running or approval-pending row reserves terminal space once.
func Measure(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64) (Usage, error) {
	var usage Usage
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
		WHERE token_id = ?`, incomingID, TerminalReservationBytes, tokenID).Scan(&usage.Rows, &usage.Bytes, &usage.Running)
	return usage, err
}
