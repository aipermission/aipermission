package db

import (
	"database/sql"
	"fmt"
	"strings"
)

func requireGloballyUniqueVaultItemNames(tx *sql.Tx) error {
	rows, err := tx.Query(`
		SELECT name
		FROM vault_items
		WHERE status = 'active'
		GROUP BY name COLLATE NOCASE
		HAVING COUNT(*) > 1
		ORDER BY lower(name), name
		LIMIT 10`)
	if err != nil {
		return fmt.Errorf("inspect active Vault item names: %w", err)
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("scan duplicate Vault item name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate duplicate Vault item names: %w", err)
	}
	if len(names) > 0 {
		return fmt.Errorf(
			"active Vault item names must be globally unique before this upgrade; rename duplicate names first: %s",
			strings.Join(names, ", "),
		)
	}
	return nil
}

func ensureVaultSessionLeaseEnvironmentHash(tx *sql.Tx) error {
	return ensureColumn(
		tx,
		"vault_session_leases",
		"environment_content_hash",
		`ALTER TABLE vault_session_leases ADD COLUMN environment_content_hash TEXT NOT NULL DEFAULT ''`,
	)
}

func ensureAuditRecoverySchema(tx *sql.Tx) error {
	required := []struct {
		table     string
		column    string
		statement string
	}{
		{"audit_logs", "event_version", `ALTER TABLE audit_logs ADD COLUMN event_version INTEGER NOT NULL DEFAULT 1`},
		{"audit_logs", "lifecycle_phase", `ALTER TABLE audit_logs ADD COLUMN lifecycle_phase TEXT NOT NULL DEFAULT ''`},
		{"audit_outbox", "next_attempt_at", `ALTER TABLE audit_outbox ADD COLUMN next_attempt_at TEXT`},
		{"audit_outbox", "dead_lettered_at", `ALTER TABLE audit_outbox ADD COLUMN dead_lettered_at TEXT`},
	}
	for _, item := range required {
		if err := ensureColumn(tx, item.table, item.column, item.statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureColumn(tx *sql.Tx, table string, column string, statement string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
		return fmt.Errorf("inspect %s.%s: %w", table, column, err)
	}
	if count > 0 {
		return nil
	}
	if _, err := tx.Exec(statement); err != nil {
		return fmt.Errorf("add %s.%s: %w", table, column, err)
	}
	return nil
}

func syncHistoryProjections(database *sql.DB) error {
	for _, statement := range historyProjectionStatements {
		if _, err := database.Exec(statement); err != nil {
			return fmt.Errorf("sync history projections: %w", err)
		}
	}
	return nil
}

const ConnectorActionOutcomeUnknownMessage = "gateway restarted while the connector action was running; inspect the target state before retrying because the remote outcome is unknown"

func runMigrationMaintenance(database *sql.DB) error {
	tx, err := database.Begin()
	if err != nil {
		return fmt.Errorf("begin migration maintenance: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, statement := range []string{
		`UPDATE console_sessions SET status = 'closed', error = 'gateway restarted', closed_at = COALESCE(closed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('connecting', 'connected')`,
		`UPDATE command_requests SET status = 'error', error = 'gateway restarted while command was running', completed_at = COALESCE(completed_at, datetime('now')) WHERE status = 'running'`,
		`UPDATE connector_action_requests SET status = 'outcome_unknown', error = '` + ConnectorActionOutcomeUnknownMessage + `', completed_at = COALESCE(completed_at, datetime('now')) WHERE status = 'running'`,
		`UPDATE vault_action_requests SET status = 'failed', error = 'gateway restarted while the Vault action was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status = 'running'`,
		`UPDATE vault_session_leases SET status = 'revoked', updated_at = datetime('now') WHERE status = 'active'`,
		`UPDATE file_transfers SET status = 'failed', error = 'gateway restarted while file transfer was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`UPDATE file_transfer_batches SET status = 'failed', error = 'gateway restarted while file transfer queue was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE status IN ('pending', 'pending_approval', 'running', 'paused')`,
		`UPDATE history_entries SET status = 'error', error = 'gateway restarted while command was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'command_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'outcome_unknown', error = '` + ConnectorActionOutcomeUnknownMessage + `', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'connector_action_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'failed', error = 'gateway restarted while the Vault action was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'vault_action_request' AND status = 'running'`,
		`UPDATE history_entries SET status = 'failed', error = 'gateway restarted while file transfer was running', completed_at = COALESCE(completed_at, datetime('now')), updated_at = datetime('now') WHERE source_ref_type = 'file_transfer' AND status IN ('pending', 'pending_approval', 'running', 'paused')`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("run migration maintenance: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration maintenance: %w", err)
	}
	return nil
}
