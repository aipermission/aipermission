package conformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPostgresRoleCleanupRejectsLateCurrentDatabaseOwnership(t *testing.T) {
	requireConformance(t)
	fixture := newPostgresRoleFenceFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	table := pgx.Identifier{"public", fixture.table}.Sanitize()
	owned := pgx.Identifier{"public", fixture.table + "_owned"}.Sanitize()
	role := pgx.Identifier{fixture.role}.Sanitize()
	for _, statement := range []string{
		"SET statement_timeout = '3s'",
		"SET lock_timeout = '500ms'",
		"INSERT INTO " + table + " VALUES (73)",
		"CREATE TABLE " + owned + " (id integer)",
		"INSERT INTO " + owned + " VALUES (37)",
		"ALTER TABLE " + owned + " OWNER TO " + role,
	} {
		if _, err := fixture.primary.Exec(ctx, statement); err != nil {
			t.Fatalf("prepare late ownership fixture: %v", err)
		}
	}

	config := fixture.config.Copy()
	config.RuntimeParams["statement_timeout"] = "3s"
	config.RuntimeParams["lock_timeout"] = "500ms"
	writerConnection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closePostgresRoleFixtureConnection(t, closeCtx, writerConnection)
	})
	writer, err := writerConnection.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cleanupPostgresRoleFenceTransaction(t, writer)
	// The completed ALTER is deliberately still uncommitted when Begin and
	// CleanupPlan run. No timing assumption is needed to establish this edge.
	if _, err := writer.Exec(ctx, "ALTER TABLE "+table+" OWNER TO "+role); err != nil {
		t.Fatalf("enter current-database ownership writer: %v", err)
	}

	cleanup, err := rolecatalog.Begin(ctx, fixture.primary)
	if cleanup != nil {
		cleanupPostgresRoleFenceTransaction(t, cleanup)
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if cleanup != nil || !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
			t.Fatalf("prior ownership writer did not cause a clean NOWAIT refusal: tx=%v error=%v", cleanup, err)
		}
		if status := fixture.primary.PgConn().TxStatus(); status != 'I' {
			t.Fatalf("NOWAIT refusal left primary transaction status %q", status)
		}
		var one int
		if err := fixture.primary.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 {
			t.Fatalf("primary is unusable after NOWAIT refusal: value=%d error=%v", one, err)
		}
		if err := writer.Commit(ctx); err != nil {
			t.Fatalf("commit writer after NOWAIT refusal: %v", err)
		}
		t.Log("NOWAIT refused the prior ownership writer; no cleanup plan or REASSIGN was executed")
		assertPostgresLateOwnershipPreserved(t, fixture)
		return
	}
	if cleanup == nil {
		t.Fatal("catalog fence admitted cleanup without a transaction")
	}
	record := fixture.record(t, cleanup)
	plan, err := rolecatalog.CleanupPlan(ctx, cleanup, record)
	if err != nil {
		t.Fatalf("plan cleanup behind prior current-database ownership writer: %v", err)
	}
	reassign := "REASSIGN OWNED BY " + role + " TO " + pgx.Identifier{record.Intent.Anchor.SuccessorName}.Sanitize()
	if len(plan) < 2 || plan[0] != reassign || plan[len(plan)-1] != "DROP ROLE "+role {
		t.Fatalf("cleanup plan lacks leading reassignment and final role drop: %#v", plan)
	}
	// Check the entire plan before executing anything, including old destructive
	// cleanup statements that might otherwise erase the late writer's table.
	for _, statement := range plan {
		if strings.Contains(strings.ToUpper(statement), "DROP OWNED") {
			t.Fatalf("cleanup plan would destroy owned objects: %s", statement)
		}
	}
	// CleanupPlan sets its own shared-row timeout. Bound object-lock waits again
	// so a native schedule that blocks REASSIGN cannot stall this regression.
	if _, err := cleanup.Exec(ctx, "SET LOCAL lock_timeout = '500ms'"); err != nil {
		t.Fatal(err)
	}
	rollback := func() {
		t.Helper()
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cleanup.Rollback(rollbackCtx); err != nil {
			t.Fatalf("rollback refused cleanup: %v", err)
		}
	}
	if _, err := cleanup.Exec(ctx, plan[0]); err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "55P03" && pgErr.Code != "40P01") {
			t.Fatalf("REASSIGN failed outside the bounded safe lock outcomes: %v", err)
		}
		rollback()
		if err := writer.Commit(ctx); err != nil {
			t.Fatalf("commit writer after bounded REASSIGN refusal: %v", err)
		}
		t.Logf("admitted fence, but REASSIGN returned %s; bounded safe rollback preserved data, late commit after successful REASSIGN was not exercised", pgErr.Code)
		assertPostgresLateOwnershipPreserved(t, fixture)
		return
	}
	var owner string
	if err := cleanup.QueryRow(ctx, `SELECT owner.rolname FROM pg_catalog.pg_class AS relation
 JOIN pg_catalog.pg_roles AS owner ON owner.oid = relation.relowner
 WHERE relation.oid = $1::pg_catalog.regclass`, owned).Scan(&owner); err != nil || owner != record.Intent.Anchor.SuccessorName {
		t.Fatalf("baseline ownership was not reassigned inside cleanup: owner=%q error=%v", owner, err)
	}
	if err := writer.Commit(ctx); err != nil {
		t.Fatalf("commit late ownership after REASSIGN: %v", err)
	}
	if err := cleanup.QueryRow(ctx, `SELECT owner.rolname FROM pg_catalog.pg_class AS relation
 JOIN pg_catalog.pg_roles AS owner ON owner.oid = relation.relowner
 WHERE relation.oid = $1::pg_catalog.regclass`, table).Scan(&owner); err != nil || owner != fixture.role {
		t.Fatalf("late table ownership did not survive REASSIGN: owner=%q error=%v", owner, err)
	}
	for index, statement := range plan[1:] {
		_, err := cleanup.Exec(ctx, statement)
		final := index == len(plan)-2
		if !final {
			if err != nil {
				t.Fatalf("cleanup failed before final ownership check: statement=%s error=%v", statement, err)
			}
			continue
		}
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "2BP01" {
			t.Fatalf("final DROP ROLE did not reject surviving table ownership: %v", err)
		}
	}
	rollback()
	t.Log("late ownership committed after REASSIGN; final DROP ROLE rejected it and rollback restored the earlier owner's table")
	assertPostgresLateOwnershipPreserved(t, fixture)
}

func assertPostgresLateOwnershipPreserved(t *testing.T, fixture postgresRoleFenceFixture) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	// Like fixture teardown, verification does not depend on a connection that
	// pgx may have closed during cancellation. Only exact fixture tables are read.
	config := fixture.config.Copy()
	config.RuntimeParams["statement_timeout"] = "3s"
	config.RuntimeParams["lock_timeout"] = "500ms"
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Fatalf("connect independent late ownership verification: %v", err)
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closePostgresRoleFixtureConnection(t, closeCtx, connection)
	}()
	for _, expected := range []struct {
		name  string
		value int
	}{{fixture.table, 73}, {fixture.table + "_owned", 37}} {
		table := pgx.Identifier{"public", expected.name}.Sanitize()
		var count, value int
		if err := connection.QueryRow(ctx, "SELECT count(*), min(id) FROM "+table).Scan(&count, &value); err != nil || count != 1 || value != expected.value {
			t.Fatalf("table data was not preserved for %s: count=%d value=%d error=%v", table, count, value, err)
		}
		var owner string
		if err := connection.QueryRow(ctx, `SELECT owner.rolname FROM pg_catalog.pg_class AS relation
 JOIN pg_catalog.pg_roles AS owner ON owner.oid = relation.relowner
 WHERE relation.oid = $1::pg_catalog.regclass`, table).Scan(&owner); err != nil || owner != fixture.role {
			t.Fatalf("committed late ownership or earlier REASSIGN rollback was lost for %s: owner=%q error=%v", table, owner, err)
		}
	}
	var exists bool
	if err := connection.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)`, fixture.role).Scan(&exists); err != nil || !exists {
		t.Fatalf("managed role did not survive refused cleanup: exists=%v error=%v", exists, err)
	}
}
