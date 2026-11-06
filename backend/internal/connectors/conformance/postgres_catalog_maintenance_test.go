package conformance_test

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRoleFenceFailsClosedDuringCatalogMaintenance(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	maintenance, err := fixture.other.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cleanupPostgresRoleFenceTransaction(t, maintenance)
	if _, err := maintenance.Exec(t.Context(), `LOCK TABLE pg_catalog.pg_database IN SHARE UPDATE EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	tx, err := rolecatalog.Begin(t.Context(), fixture.primary)
	if tx != nil {
		_ = tx.Rollback(t.Context())
		t.Fatal("catalog maintenance exposed an unfenced transaction")
	}
	var serverError *pgconn.PgError
	if !errors.As(err, &serverError) || serverError.Code != "55P03" {
		t.Fatalf("maintenance lock did not fail closed: %v", err)
	}
	logPostgresFixtureCatalogLocks(t, fixture.primary)
	assertPostgresRoleFenceConnectionIdle(t, fixture.primary)
	if err := maintenance.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	_ = beginPostgresRoleFence(t, fixture.primary).Rollback(t.Context())
}
