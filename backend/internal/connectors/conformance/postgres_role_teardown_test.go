package conformance_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func closePostgresRoleFixtureConnection(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	if connection == nil {
		return
	}
	if err := connection.Close(ctx); err != nil {
		t.Errorf("close role fixture connection: %v", err)
	}
	select {
	case <-connection.PgConn().CleanupDone():
	case <-ctx.Done():
		t.Errorf("role fixture connection cleanup did not finish: %v", ctx.Err())
	}
}

func (fixture postgresRoleFenceFixture) cleanup(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	closePostgresRoleFixtureConnection(t, ctx, fixture.primary)
	closePostgresRoleFixtureConnection(t, ctx, fixture.other)
	// Query cancellation can permanently close either test connection. Teardown
	// must not depend on those connections or silently swallow their failures.
	connection, err := pgx.ConnectConfig(ctx, fixture.config.Copy())
	if err != nil {
		t.Errorf("connect independent role fixture teardown: %v", err)
		return
	}
	defer closePostgresRoleFixtureConnection(t, ctx, connection)
	for _, statement := range fixture.cleanupStatements() {
		if _, err := connection.Exec(ctx, statement); err != nil {
			t.Errorf("clean role fixture with %s: %v", statement, err)
		}
	}
	fixture.cleanupRoles(t, ctx, connection)
	fixture.assertAbsent(t, ctx, connection)
}

func (fixture postgresRoleFenceFixture) cleanupStatements() []string {
	statements := []string{
		"DROP TABLESPACE IF EXISTS " + pgx.Identifier{fixture.role + "_space"}.Sanitize(),
		"DROP TABLE IF EXISTS " + pgx.Identifier{"public", fixture.table + "_owned"}.Sanitize(),
		"DROP TABLE IF EXISTS " + pgx.Identifier{"public", fixture.table}.Sanitize(),
	}
	for _, database := range fixture.databaseNames() {
		statements = append(statements, "DROP DATABASE IF EXISTS "+pgx.Identifier{database}.Sanitize())
	}
	return statements
}

func (fixture postgresRoleFenceFixture) cleanupRoles(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	for _, role := range fixture.roleNames() {
		var exists bool
		if err := connection.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)`, role).Scan(&exists); err != nil {
			t.Errorf("read fixture role before teardown: %v", err)
			continue
		}
		if !exists {
			continue
		}
		for _, statement := range []string{
			"REVOKE ALL PRIVILEGES ON SCHEMA public FROM " + pgx.Identifier{role}.Sanitize(),
			"REVOKE ALL PRIVILEGES ON DATABASE " + pgx.Identifier{fixture.config.Database}.Sanitize() + " FROM " + pgx.Identifier{role}.Sanitize(),
			"DROP ROLE " + pgx.Identifier{role}.Sanitize(),
		} {
			if _, err := connection.Exec(ctx, statement); err != nil {
				t.Errorf("clean fixture role with %s: %v", statement, err)
			}
		}
	}
}

func (fixture postgresRoleFenceFixture) databaseNames() []string {
	return []string{fixture.database, fixture.database + "_idle", fixture.database + "_idle_renamed"}
}

func (fixture postgresRoleFenceFixture) roleNames() []string {
	return []string{fixture.role, fixture.role + "_renamed", fixture.member, fixture.member + "_created"}
}

func (fixture postgresRoleFenceFixture) assertAbsent(t *testing.T, ctx context.Context, connection *pgx.Conn) {
	t.Helper()
	var remains bool
	err := connection.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = ANY($1::text[])) OR
 EXISTS (SELECT 1 FROM pg_catalog.pg_database WHERE datname = ANY($2::text[])) OR
 EXISTS (SELECT 1 FROM pg_catalog.pg_tablespace WHERE spcname = $3) OR
 EXISTS (SELECT 1 FROM pg_catalog.pg_class AS relation
   JOIN pg_catalog.pg_namespace AS namespace ON namespace.oid = relation.relnamespace
   WHERE namespace.nspname = 'public' AND relation.relname = ANY($4::text[]))`,
		fixture.roleNames(), fixture.databaseNames(), fixture.role+"_space", []string{fixture.table, fixture.table + "_owned"}).Scan(&remains)
	if err != nil || remains {
		t.Errorf("fixture-owned catalog objects survived teardown: remains=%v error=%v", remains, err)
	}
}
