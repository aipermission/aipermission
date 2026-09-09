package accesscontrol

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "access-control.db"), "AccessControlPassword123")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

func auditRunner(database *sql.DB, failAfterMutation error) MutationRunner {
	return func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
		tx, err := database.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if err := mutate(tx); err != nil {
			return err
		}
		if failAfterMutation != nil {
			return failAfterMutation
		}
		encoded, err := json.Marshal(payload())
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO audit_logs (actor_type, action, payload_json, created_at)
			VALUES ('user', ?, ?, ?)`, action, string(encoded), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
		return tx.Commit()
	}
}

func countRows(t *testing.T, database *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}
