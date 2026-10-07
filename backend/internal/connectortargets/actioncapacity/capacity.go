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
	return Within(ctx, executor, tokenID, incomingID, DefaultLimits())
}

func DefaultLimits() Limits {
	return Limits{Rows: MaxRows, Bytes: MaxBytes, Running: MaxRunning}
}

// LimitReason distinguishes active work from retained storage without exposing payloads.
func (usage Usage) LimitReason(limits Limits) string {
	if usage.Rows > limits.Rows {
		return "stored_rows"
	}
	if usage.Bytes > limits.Bytes {
		return "stored_bytes"
	}
	if usage.Running > limits.Running {
		return "running_requests"
	}
	return ""
}

func Within(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64, limits Limits) (bool, error) {
	usage, err := Measure(ctx, executor, tokenID, incomingID)
	if err != nil {
		return false, err
	}
	return usage.LimitReason(limits) == "", nil
}

// Measure requires a configured executor; it neither opens nor commits a transaction.
// Each incoming, running or approval-pending row reserves terminal space once.
func Measure(ctx context.Context, executor sqldb.Executor, tokenID, incomingID int64) (Usage, error) {
	var usage Usage
	err := executor.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(
			stored_bytes +
			CASE WHEN request_id = ? OR status IN ('running', 'approval_pending') THEN ? ELSE 0 END
		), 0), COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0)
		FROM connector_action_request_usage
		WHERE token_id = ?`, incomingID, TerminalReservationBytes, tokenID).Scan(&usage.Rows, &usage.Bytes, &usage.Running)
	return usage, err
}
