package conformance_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func createPostgresSharedOwnershipFixture(t *testing.T, fixture postgresRoleFenceFixture) string {
	t.Helper()
	name := fixture.role + "_space"
	if _, err := fixture.primary.Exec(t.Context(), `SET allow_in_place_tablespaces = on`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.primary.Exec(t.Context(), "CREATE TABLESPACE "+pgx.Identifier{name}.Sanitize()+" LOCATION ''"); err != nil {
		t.Fatal(err)
	}
	return name
}

// Observe the real blocking edge instead of relying on timing/sleep to infer
// that cleanup reached its shared-row drain after acquiring the relation fence.
func waitPostgresSharedOwnershipBlocker(t *testing.T, writer pgx.Tx, writerPID, cleanupPID uint32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := writer.QueryRow(ctx, `SELECT $1::integer = ANY(pg_catalog.pg_blocking_pids($2::integer))`, writerPID, cleanupPID).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("cleanup never reached the prior ownership writer's row lock")
		case <-ticker.C:
		}
	}
}
