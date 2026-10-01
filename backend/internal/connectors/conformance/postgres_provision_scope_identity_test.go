package conformance_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	"github.com/jackc/pgx/v5"
)

func TestPostgresProvisionScopePreservesExactPrivilegesRealService(t *testing.T) {
	requireConformance(t)
	connection := connectPostgresPolicyFixture(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closePostgresRoleFixtureConnection(t, ctx, connection)
	})
	suffix := fmt.Sprintf("ap_scope_%d", time.Now().UnixNano())
	schema, table := " "+suffix+"$aipermission$ ", ` x\' OR owned.sequence_name IS NOT NULL -- `
	admin := suffix + "_admin"
	roles := []string{suffix + "_on_reader", suffix + "_on_writer", suffix + "_off_reader", suffix + "_off_writer", admin}
	schemas := []string{schema, strings.TrimSpace(schema)}
	tables := []string{table, strings.TrimSpace(table)}
	t.Cleanup(func() { cleanupPostgresExactGrantFixture(t, connection.Config().Copy(), schemas, roles) })
	if _, err := connection.Exec(t.Context(), "CREATE ROLE "+pgx.Identifier{admin}.Sanitize()+" LOGIN SUPERUSER PASSWORD 'conformance-only'"); err != nil {
		t.Fatal(err)
	}
	for _, name := range schemas {
		if _, err := connection.Exec(t.Context(), "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Fatal(err)
		}
		for _, item := range tables {
			statement := "CREATE TABLE " + pgx.Identifier{name, item}.Sanitize() + ` (" id " integer, id integer, sequence_id serial)`
			if _, err := connection.Exec(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	for modeIndex, mode := range []string{"on", "off"} {
		if _, err := connection.Exec(t.Context(), "ALTER ROLE "+pgx.Identifier{admin}.Sanitize()+" SET standard_conforming_strings = '"+mode+"'"); err != nil {
			t.Fatal(err)
		}
		assertPostgresScopeAdminStringSetting(t, connection.Config().Copy(), admin, mode)
		for index, preset := range []string{"read_only", "read_write"} {
			role := roles[modeIndex*2+index]
			t.Run(mode+"/"+preset, func(t *testing.T) {
				runtime := postgresConformanceRuntime(t)
				runtime.Profile.Public = map[string]any{"username": admin}
				journal := rolejournal.New(newPostgresReconciliationStore(t))
				runtime.Capabilities = postgresOperatorCapabilities{runtime.Capabilities, journal}
				provisioned, err := postgresconnector.New().ProvisionCredentialProfile(t.Context(), runtime, map[string]any{
					"role_name": role, "preset": preset,
					"scope": map[string]any{"schemas": []any{map[string]any{
						"schema": schema, "tables": []any{map[string]any{
							"table": table, "all_columns": preset == "read_write", "columns": []any{" id "},
						}},
					}}},
				})
				if err != nil || provisioned.Public["username"] != role {
					t.Fatalf("provision exact scope: %v", err)
				}
				for _, name := range schemas {
					for _, item := range tables {
						for _, column := range []string{" id ", "id"} {
							want := name == schema && item == table && (column == " id " || preset == "read_write")
							assertPostgresExactColumnGrant(t, connection, role, name, item, column, want)
						}
						assertPostgresExactSequenceGrant(t, connection, role, name, item, preset == "read_write" && name == schema && item == table)
					}
				}
			})
		}
	}
}

func assertPostgresScopeAdminStringSetting(t *testing.T, config *pgx.ConnConfig, admin, want string) {
	t.Helper()
	config.User, config.Password = admin, "conformance-only"
	connection, err := pgx.ConnectConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		closePostgresRoleFixtureConnection(t, ctx, connection)
	}()
	var actual string
	if err := connection.QueryRow(t.Context(), "SHOW standard_conforming_strings").Scan(&actual); err != nil || actual != want {
		t.Fatalf("scope admin startup setting: got=%q want=%q err=%v", actual, want, err)
	}
}

func assertPostgresExactSequenceGrant(t *testing.T, connection *pgx.Conn, role, schema, table string, want bool) {
	t.Helper()
	var allowed bool
	err := connection.QueryRow(t.Context(), `SELECT has_sequence_privilege($1, pg_get_serial_sequence($2, 'sequence_id'), 'USAGE')`,
		role, pgx.Identifier{schema, table}.Sanitize()).Scan(&allowed)
	if err != nil || allowed != want {
		t.Fatalf("sequence identity drift for %q/%q: allowed=%v want=%v err=%v", schema, table, allowed, want, err)
	}
}

func assertPostgresExactColumnGrant(t *testing.T, connection *pgx.Conn, role, schema, table, column string, want bool) {
	t.Helper()
	var allowed bool
	err := connection.QueryRow(t.Context(), `SELECT has_column_privilege($1, $2, $3, 'SELECT')`,
		role, pgx.Identifier{schema, table}.Sanitize(), column).Scan(&allowed)
	if err != nil || allowed != want {
		t.Fatalf("privilege identity drift for %q/%q/%q: allowed=%v want=%v err=%v", schema, table, column, allowed, want, err)
	}
}

func cleanupPostgresExactGrantFixture(t *testing.T, config *pgx.ConnConfig, schemas, roles []string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	connection, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		t.Errorf("connect exact-grant fixture teardown: %v", err)
		return
	}
	defer closePostgresRoleFixtureConnection(t, ctx, connection)
	for _, schema := range schemas {
		if _, err := connection.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Errorf("remove owned grant fixture schema: %v", err)
		}
	}
	for _, role := range roles {
		var exists bool
		if err := connection.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = $1)`, role).Scan(&exists); err != nil {
			t.Errorf("read owned grant fixture role: %v", err)
			continue
		}
		if exists {
			for _, statement := range []string{
				"REVOKE ALL PRIVILEGES ON DATABASE " + pgx.Identifier{config.Database}.Sanitize() + " FROM " + pgx.Identifier{role}.Sanitize(),
				"DROP ROLE " + pgx.Identifier{role}.Sanitize(),
			} {
				if _, err := connection.Exec(ctx, statement); err != nil {
					t.Errorf("remove owned grant fixture role: %v", err)
				}
			}
		}
	}
	var remains bool
	err = connection.QueryRow(ctx, `SELECT
 EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = ANY($1::text[])) OR
 EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = ANY($2::text[]))`, schemas, roles).Scan(&remains)
	if err != nil || remains {
		t.Errorf("owned exact-grant fixture survived teardown: remains=%v error=%v", remains, err)
	}
}
