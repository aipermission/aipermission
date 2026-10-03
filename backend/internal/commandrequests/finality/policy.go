// Package finality owns durable dispatch evidence and command recovery policy.
// Admission proves permission to send, not remote delivery or success.
package finality

const DispatchColumn = `ALTER TABLE command_requests ADD COLUMN dispatch_state TEXT NOT NULL DEFAULT 'unknown' CHECK (dispatch_state IN ('unknown', 'queued', 'admitted'));`

const ClaimSQL = `UPDATE command_requests SET dispatch_state = 'admitted'
	WHERE id = ? AND status = 'running' AND dispatch_state = 'queued'`

func InitialState(queued bool) string {
	if queued {
		return "queued"
	}
	return "unknown"
}

// RecoverySQL is shared by shutdown and encrypted-database reopening. The WHERE
// clause is caller-owned SQL, never request data. Bind the reason first, followed
// by any WHERE arguments. A bound session overrides contradictory queue evidence.
func RecoverySQL(where string) string {
	return `UPDATE command_requests SET
		status = CASE WHEN dispatch_state = 'queued' AND COALESCE(session_id, 0) = 0 THEN 'canceled' ELSE 'outcome_unknown' END,
		error = ? || CASE WHEN dispatch_state = 'queued' AND COALESCE(session_id, 0) = 0 THEN '' ELSE '; remote outcome is unknown; inspect the existing console session and external state before retrying' END,
		exit_code = NULL,
		completed_at = COALESCE(completed_at, strftime('%Y-%m-%dT%H:%M:%f000000Z', 'now'))
		WHERE ` + where
}
