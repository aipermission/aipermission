package db

import (
	"database/sql"
	"fmt"
)

var connectorActionExecutionClaimMigration = migration{
	version:     24,
	description: "add durable connector action execution claims",
	preflight:   ensureConnectorActionExecutionClaimColumns,
	statements: []string{
		`CREATE INDEX IF NOT EXISTS idx_connector_action_requests_execution_lease
		 ON connector_action_requests(status, execution_lease_expires_at)`,
	},
}

func ensureConnectorActionExecutionClaimColumns(tx *sql.Tx) error {
	var dispatchColumnCount int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('connector_action_requests') WHERE name = 'dispatch_started_at'`).Scan(&dispatchColumnCount); err != nil {
		return fmt.Errorf("inspect connector action dispatch marker: %w", err)
	}
	for _, item := range []struct {
		column    string
		statement string
	}{
		{"execution_owner", `ALTER TABLE connector_action_requests ADD COLUMN execution_owner TEXT NOT NULL DEFAULT ''`},
		{"execution_lease_expires_at", `ALTER TABLE connector_action_requests ADD COLUMN execution_lease_expires_at TEXT NOT NULL DEFAULT ''`},
		{"dispatch_started_at", `ALTER TABLE connector_action_requests ADD COLUMN dispatch_started_at TEXT NOT NULL DEFAULT ''`},
	} {
		if err := ensureColumn(tx, "connector_action_requests", item.column, item.statement); err != nil {
			return err
		}
	}
	if dispatchColumnCount == 0 {
		// A pre-v24 running row may already have crossed the external dispatch
		// boundary. Mark it conservatively so recovery never invites a retry.
		if _, err := tx.Exec(`
			UPDATE connector_action_requests
			SET dispatch_started_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
			WHERE status = 'running' AND dispatch_started_at = ''`); err != nil {
			return fmt.Errorf("backfill legacy connector action dispatch markers: %w", err)
		}
	}
	return nil
}
