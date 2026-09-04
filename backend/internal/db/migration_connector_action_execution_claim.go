package db

import "database/sql"

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
	return nil
}
