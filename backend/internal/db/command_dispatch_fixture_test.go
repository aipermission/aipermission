package db

import (
	"database/sql"
	"testing"
)

func removeCommandDispatchEvidence(t *testing.T, database *sql.DB) {
	t.Helper()
	if _, err := database.Exec(`ALTER TABLE command_requests DROP COLUMN dispatch_state`); err != nil {
		t.Fatalf("remove current command dispatch evidence from previous-schema fixture: %v", err)
	}
}
