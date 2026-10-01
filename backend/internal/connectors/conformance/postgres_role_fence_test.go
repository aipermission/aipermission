package conformance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRoleFenceBlocksCrossDatabaseCatalogWriters(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	tx := beginPostgresRoleFence(t, fixture.primary)
	record := fixture.record(t, tx)
	if err := rolecatalog.VerifyRole(t.Context(), tx, record); err != nil {
		t.Fatal(err)
	}
	role, member := pgx.Identifier{fixture.role}.Sanitize(), pgx.Identifier{fixture.member}.Sanitize()
	probes := []struct{ name, sql string }{
		{"role rename", `ALTER ROLE ` + role + ` RENAME TO ` + pgx.Identifier{fixture.role + "_renamed"}.Sanitize()},
		{"role alter", `ALTER ROLE ` + role + ` LOGIN`},
		{"role drop", `DROP ROLE ` + role},
		{"marker replacement", `COMMENT ON ROLE ` + role + ` IS 'foreign marker'`},
		{"membership drift", `GRANT ` + role + ` TO ` + member},
		{"database rename", `ALTER DATABASE ` + pgx.Identifier{fixture.database + "_idle"}.Sanitize() + ` RENAME TO ` + pgx.Identifier{fixture.database + "_idle_renamed"}.Sanitize()},
		{"role creation", `CREATE ROLE ` + pgx.Identifier{fixture.member + "_created"}.Sanitize()},
	}
	t.Run("tablespace catalog writer", func(t *testing.T) {
		writer, err := fixture.other.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		cleanupPostgresRoleFenceTransaction(t, writer)
		_, err = writer.Exec(t.Context(), `LOCK TABLE pg_catalog.pg_tablespace IN ROW EXCLUSIVE MODE`)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("tablespace writer escaped the held catalog fence: %v", err)
		}
		if err := writer.Rollback(t.Context()); err != nil {
			t.Fatal(err)
		}
	})
	for _, probe := range probes {
		t.Run(probe.name, func(t *testing.T) {
			_, err := fixture.other.Exec(t.Context(), probe.sql)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
				t.Fatalf("writer was not rejected specifically by the held cross-database lock: %v", err)
			}
			if err := rolecatalog.VerifyRole(t.Context(), tx, record); err != nil {
				t.Fatalf("identity changed under fence: %v", err)
			}
		})
	}
	// Ownership and ACL drift involve current-database objects, not merely roles.
	owner := connectPostgresPolicyFixture(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = owner.Close(ctx)
	})
	if _, err := owner.Exec(t.Context(), `SET lock_timeout = '250ms'`); err != nil {
		t.Fatal(err)
	}
	table := pgx.Identifier{"public", fixture.table}.Sanitize()
	for _, statement := range []string{`ALTER TABLE ` + table + ` OWNER TO ` + role, `GRANT SELECT ON ` + table + ` TO ` + role} {
		_, err := owner.Exec(t.Context(), statement)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("ownership/ACL dependency writer escaped fence: %v", err)
		}
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.other.Exec(t.Context(), `ALTER ROLE `+role+` LOGIN`); err != nil {
		t.Fatalf("catalog locks survived rollback: %v", err)
	}
}

func TestPostgresRoleFenceFailsClosedWhenCatalogWriterAlreadyEntered(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	writer, err := fixture.other.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cleanupPostgresRoleFenceTransaction(t, writer)
	if _, err := writer.Exec(t.Context(), `ALTER ROLE `+pgx.Identifier{fixture.role}.Sanitize()+` LOGIN`); err != nil {
		t.Fatal(err)
	}
	tx, err := rolecatalog.Begin(t.Context(), fixture.primary)
	if tx != nil {
		cleanupPostgresRoleFenceTransaction(t, tx)
		t.Fatal("failed NOWAIT fence exposed a transaction")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("existing writer did not fail the fence: %v", err)
	}
	assertPostgresRoleFenceConnectionIdle(t, fixture.primary)
}

func TestPostgresRoleFenceRejectsRoleNameReuseAndMarkerDrift(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	tx := beginPostgresRoleFence(t, fixture.primary)
	record := fixture.record(t, tx)
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	role := pgx.Identifier{fixture.role}.Sanitize()
	if _, err := fixture.primary.Exec(t.Context(), `DROP ROLE `+role); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.primary.Exec(t.Context(), `CREATE ROLE `+role); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.primary.Exec(t.Context(), `COMMENT ON ROLE `+role+` IS '`+fixture.marker+`'`); err != nil {
		t.Fatal(err)
	}
	tx = beginPostgresRoleFence(t, fixture.primary)
	if err := rolecatalog.VerifyRole(t.Context(), tx, record); !errors.Is(err, rolecatalog.ErrIdentityDrift) {
		t.Fatalf("replacement role with copied marker accepted: %v", err)
	}
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	tx = beginPostgresRoleFence(t, fixture.primary)
	record = fixture.record(t, tx)
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.primary.Exec(t.Context(), `COMMENT ON ROLE `+role+` IS 'foreign marker'`); err != nil {
		t.Fatal(err)
	}
	tx = beginPostgresRoleFence(t, fixture.primary)
	if err := rolecatalog.VerifyRole(t.Context(), tx, record); !errors.Is(err, rolecatalog.ErrIdentityDrift) {
		t.Fatalf("changed marker accepted: %v", err)
	}
}
