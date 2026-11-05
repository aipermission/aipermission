package conformance_test

import (
	"context"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/jackc/pgx/v5"
)

type postgresExecutionFixture struct {
	sqlExecutionFixture
	observer *pgx.Conn
}

func newPostgresExecutionFixture(t *testing.T) postgresExecutionFixture {
	t.Helper()
	requireConformance(t)
	name := sqlExecutionName()
	fixture := postgresExecutionFixture{
		sqlExecutionFixture: sqlExecutionFixture{
			connector: postgresconnector.New(), runtime: postgresConformanceRuntime(t),
			table: pgx.Identifier{"public", name}.Sanitize(), procedure: pgx.Identifier{"public", name + "_proc"}.Sanitize(),
		},
		observer: connectPostgresPolicyFixture(t),
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		defer fixture.observer.Close(ctx)
		for _, statement := range []string{"DROP PROCEDURE IF EXISTS " + fixture.procedure + "()", "DROP TABLE IF EXISTS " + fixture.table} {
			if _, err := fixture.observer.Exec(ctx, statement); err != nil {
				t.Errorf("cleanup owned SQL execution fixture: %v", err)
			}
		}
	})
	if _, err := fixture.observer.Exec(t.Context(), "SET lock_timeout='1s'; SET statement_timeout='5s'"); err != nil {
		t.Fatal(err)
	}
	runSQLExecution(t, fixture.sqlExecutionFixture,
		"CREATE TABLE "+fixture.table+" (id int PRIMARY KEY, value int NOT NULL); INSERT INTO "+fixture.table+" VALUES (1,0);", 100)
	return fixture
}

func (fixture postgresExecutionFixture) value(t *testing.T) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	var value int
	if err := fixture.observer.QueryRow(ctx, "SELECT value FROM "+fixture.table+" WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (fixture postgresExecutionFixture) readonly(t *testing.T) sqlExecutionFixture {
	t.Helper()
	name := sqlExecutionName() + "_reader"
	role := pgx.Identifier{name}.Sanitize()
	password := "Fixture-only-Execute-Reader1!"
	if _, err := fixture.observer.Exec(t.Context(), "CREATE ROLE "+role+" LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		for _, statement := range []string{
			"REVOKE SELECT ON " + fixture.table + " FROM " + role,
			"REVOKE USAGE ON SCHEMA public FROM " + role, "DROP ROLE " + role,
		} {
			if _, err := fixture.observer.Exec(ctx, statement); err != nil {
				t.Errorf("cleanup owned readonly execution role: %v", err)
			}
		}
	})
	if _, err := fixture.observer.Exec(t.Context(), "GRANT USAGE ON SCHEMA public TO "+role+"; GRANT SELECT ON "+fixture.table+" TO "+role); err != nil {
		t.Fatal(err)
	}
	reader := fixture.sqlExecutionFixture
	reader.runtime.Profile = connectors.CredentialProfileView{ID: 701, TargetID: reader.runtime.Target.ID,
		ConnectorKind: postgresconnector.Kind, Kind: "username_password", Label: "owned readonly execution fixture",
		Public: map[string]any{"username": name}}
	reader.runtime.Target.Ref = connectors.FormatTargetRef(postgresconnector.Kind, reader.runtime.Target.ID, reader.runtime.Profile.ID)
	reader.runtime.Secrets = fixtureSecrets{"password": password}
	return reader
}
