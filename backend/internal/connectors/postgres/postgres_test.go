package postgresconnector

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/connectortest"
)

func TestConnectorMetadataAndSchemas(t *testing.T) {
	connector := New()
	if connector.Kind() != Kind || connector.Label() != Label || connector.Version() == "" {
		t.Fatalf("unexpected metadata kind=%q label=%q version=%q", connector.Kind(), connector.Label(), connector.Version())
	}

	targetSchema := connector.TargetSchema()
	if !hasField(targetSchema, "connection_mode") || !hasField(targetSchema, "host") || !hasField(targetSchema, "database") {
		t.Fatalf("expected connection_mode, host, and database target fields, got %#v", targetSchema.Fields)
	}
	if !hasField(targetSchema, "transport_target_ref") {
		t.Fatalf("expected transport_target_ref field for tunneled connections, got %#v", targetSchema.Fields)
	}
	if !fieldOptionsContain(targetSchema, "connection_mode", "over_ssh") {
		t.Fatalf("target schema should advertise supported over_ssh mode: %#v", targetSchema.Fields)
	}
	if defaultFieldValue(targetSchema, "ssl_mode") != "auto" {
		t.Fatalf("postgres ssl_mode should default to auto, got %#v", defaultFieldValue(targetSchema, "ssl_mode"))
	}

	credentialSchemas := connector.CredentialSchemas()
	if len(credentialSchemas) != 1 || credentialSchemas[0].Kind != "username_password" {
		t.Fatalf("unexpected credential schemas: %#v", credentialSchemas)
	}
	if !hasField(credentialSchemas[0].Schema, "username") || !hasField(credentialSchemas[0].Schema, "password") {
		t.Fatalf("expected username and password credential fields, got %#v", credentialSchemas[0].Schema.Fields)
	}
}

func TestSSLModeUsesVerifiedTLSForDirectRemoteTargets(t *testing.T) {
	tests := []struct {
		name   string
		target connectors.TargetView
		want   string
	}{
		{name: "direct remote auto", target: connectors.TargetView{Config: map[string]any{"connection_mode": "direct", "host": "db.example.com", "ssl_mode": "auto"}}, want: "verify-full"},
		{name: "legacy direct remote omitted", target: connectors.TargetView{Config: map[string]any{"connection_mode": "direct", "host": "db.example.com"}}, want: "require"},
		{name: "direct loopback auto", target: connectors.TargetView{Config: map[string]any{"connection_mode": "direct", "host": "127.0.0.1", "ssl_mode": "auto"}}, want: "require"},
		{name: "over SSH auto", target: connectors.TargetView{Config: map[string]any{"connection_mode": "over_ssh", "host": "127.0.0.1", "ssl_mode": "auto"}}, want: "require"},
		{name: "explicit require preserved", target: connectors.TargetView{Config: map[string]any{"connection_mode": "direct", "host": "db.example.com", "ssl_mode": "require"}}, want: "require"},
		{name: "explicit verify full", target: connectors.TargetView{Config: map[string]any{"connection_mode": "direct", "host": "db.example.com", "ssl_mode": "verify_full"}}, want: "verify-full"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := sslMode(test.target); got != test.want {
				t.Fatalf("sslMode() = %q; want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeTargetConfigUpdatePreservesLegacyAndExplicitTLSModes(t *testing.T) {
	connector := New()
	legacy := connector.NormalizeTargetConfigUpdate(
		map[string]any{"host": "db.example.com"},
		map[string]any{"host": "db-new.example.com"},
	)
	if legacy["ssl_mode"] != "require" {
		t.Fatalf("legacy ssl_mode = %#v", legacy["ssl_mode"])
	}
	explicit := connector.NormalizeTargetConfigUpdate(
		map[string]any{"ssl_mode": "require"},
		map[string]any{"ssl_mode": "verify_full"},
	)
	if explicit["ssl_mode"] != "verify_full" {
		t.Fatalf("explicit ssl_mode = %#v", explicit["ssl_mode"])
	}
	for name, value := range map[string]any{"null": nil, "blank": "  "} {
		t.Run(name, func(t *testing.T) {
			normalized := connector.NormalizeTargetConfigUpdate(
				map[string]any{"ssl_mode": "prefer"},
				map[string]any{"host": "db-new.example.com", "ssl_mode": value},
			)
			if normalized["ssl_mode"] != "prefer" {
				t.Fatalf("preserved ssl_mode = %#v", normalized["ssl_mode"])
			}
		})
	}
}

func TestPostgresCLIUsesSystemRootsForVerifiedTLS(t *testing.T) {
	t.Setenv("PGHOSTADDR", "attacker.invalid")
	t.Setenv("PGOPTIONS", "-c search_path=attacker")
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{Config: map[string]any{
			"connection_mode": "direct", "host": "db.example.com", "port": 5432,
			"database": "app", "ssl_mode": "auto",
		}},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "reader"}},
		Secrets: fakeSecrets{"password": "secret"},
	}
	invocation, err := postgresCLIConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("postgres CLI connection: %v", err)
	}
	defer invocation.Cleanup()
	if !containsString(invocation.Env, "PGSSLMODE=verify-full") || !containsString(invocation.Env, "PGSSLROOTCERT=system") {
		t.Fatalf("verified CLI environment missing system roots: %#v", invocation.Env)
	}
	if containsPrefix(invocation.Env, "PGHOSTADDR=") {
		t.Fatalf("direct CLI connection unexpectedly set PGHOSTADDR: %#v", invocation.Env)
	}
	if containsPrefix(invocation.Env, "PGOPTIONS=") {
		t.Fatalf("direct CLI connection inherited ambient PGOPTIONS: %#v", invocation.Env)
	}
	assertPostgresCLIConnectionURL(t, invocation.Args, "db.example.com", 5432, "reader", "app")
}

func TestPostgresCLIConnectionURLPreservesLiteralDatabaseNames(t *testing.T) {
	tests := []string{
		"app",
		"review=db",
		"host=/tmp/socket port=2 dbname=postgres",
		"database with spaces",
		"folder/name?sslmode=disable#fragment",
		`quote'and\\backslash`,
	}
	for _, database := range tests {
		t.Run(database, func(t *testing.T) {
			args := []string{"--dbname", postgresCLIConnectionURL("db.example.com", 5432, "reader", database), "--no-password"}
			assertPostgresCLIConnectionURL(t, args, "db.example.com", 5432, "reader", database)
		})
	}
}

func TestPostgresCLIOverSSHPreservesTLSHostIdentity(t *testing.T) {
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{Ref: "postgres:1:1", Config: map[string]any{
			"connection_mode": "over_ssh", "transport_target_ref": "ssh:2:2",
			"host": "db.internal.example", "port": 5432, "database": "app", "ssl_mode": "verify_full",
		}},
		Profile:      connectors.CredentialProfileView{Public: map[string]any{"username": "reader"}},
		Secrets:      fakeSecrets{"password": "secret"},
		Capabilities: fakePostgresCapabilities{transport: noDialPostgresTransport{}},
	}
	invocation, err := postgresCLIConnection(context.Background(), runtime)
	if err != nil {
		t.Fatalf("postgres CLI connection: %v", err)
	}
	defer invocation.Cleanup()
	if !containsString(invocation.Env, "PGSSLMODE=verify-full") || !containsString(invocation.Env, "PGSSLROOTCERT=system") || !containsPrefix(invocation.Env, "PGHOSTADDR=127.0.0.1") {
		t.Fatalf("tunneled CLI environment does not separate TLS identity and dial address: %#v", invocation.Env)
	}
	connectionURL := assertPostgresCLIConnectionURL(t, invocation.Args, "db.internal.example", 0, "reader", "app")
	if connectionURL.Port() == "5432" {
		t.Fatalf("tunneled CLI URL retained remote port instead of local forwarded port: %q", connectionURL.String())
	}
}

func assertPostgresCLIConnectionURL(t *testing.T, args []string, wantHost string, wantPort int, wantUser, wantDatabase string) *url.URL {
	t.Helper()
	rawURL := argumentValue(args, "--dbname")
	connectionURL, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse Postgres CLI URL %q: %v", rawURL, err)
	}
	if connectionURL.Scheme != "postgresql" {
		t.Fatalf("CLI URL scheme = %q, want postgresql", connectionURL.Scheme)
	}
	if connectionURL.Hostname() != wantHost || connectionURL.User == nil || connectionURL.User.Username() != wantUser {
		t.Fatalf("CLI URL connection identity = %q, want host=%q user=%q", connectionURL.String(), wantHost, wantUser)
	}
	if wantPort > 0 && connectionURL.Port() != strconv.Itoa(wantPort) {
		t.Fatalf("CLI URL port = %q, want %d", connectionURL.Port(), wantPort)
	}
	if strings.TrimPrefix(connectionURL.Path, "/") != wantDatabase {
		t.Fatalf("CLI URL database = %q, want literal %q", strings.TrimPrefix(connectionURL.Path, "/"), wantDatabase)
	}
	if _, passwordPresent := connectionURL.User.Password(); passwordPresent || connectionURL.Query().Has("password") || strings.Contains(rawURL, "secret") {
		t.Fatalf("CLI URL must not contain a password: %q", rawURL)
	}
	return connectionURL
}

func TestGetHelpAndActionList(t *testing.T) {
	connector := New()
	target := connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind}

	help, err := connector.GetHelp(context.Background(), target)
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	if help.ConnectorID != Kind || !strings.Contains(help.Title, "main-db") || len(help.Usage) == 0 || len(help.Warnings) == 0 {
		t.Fatalf("unexpected help: %#v", help)
	}

	actions, err := connector.GetActionList(context.Background(), target, connectors.CredentialProfileView{ConnectorKind: Kind, Kind: "username_password"})
	if err != nil {
		t.Fatalf("action list: %v", err)
	}
	connectortest.AssertActionListStable(t, connector, target, connectors.CredentialProfileView{ConnectorKind: Kind, Kind: "username_password"})
	if len(actions) != 4 {
		t.Fatalf("expected 4 actions, got %d", len(actions))
	}
	for index, want := range []string{ActionGetSchemas, ActionGetTables, ActionDescribeTable, ActionQueryReadonly} {
		if actions[index].Name != want || actions[index].Risk != connectors.RiskRead {
			t.Fatalf("action[%d] = %#v", index, actions[index])
		}
	}
}

func TestPrepareMetadataActions(t *testing.T) {
	connector := New()
	req := connectors.ActionRequest{
		Target:  connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind},
		Profile: connectors.CredentialProfileView{ID: 11, ConnectorKind: Kind, Kind: "username_password", Label: "readonly"},
	}

	schemas, err := connector.PrepareAction(context.Background(), withAction(req, ActionGetSchemas, nil))
	if err != nil {
		t.Fatalf("prepare get_schemas: %v", err)
	}
	if schemas.ActionName != ActionGetSchemas || schemas.ProfileID != 11 || schemas.Risk != connectors.RiskRead {
		t.Fatalf("unexpected schemas action: %#v", schemas)
	}

	tables, err := connector.PrepareAction(context.Background(), withAction(req, ActionGetTables, map[string]any{
		"schema":         " public ",
		"include_system": "true",
	}))
	if err != nil {
		t.Fatalf("prepare get_tables: %v", err)
	}
	if tables.Payload["schema"] != "public" || tables.Payload["include_system"] != true {
		t.Fatalf("unexpected tables payload: %#v", tables.Payload)
	}
}

func TestPrepareDescribeTable(t *testing.T) {
	prepared, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind},
		Profile:    connectors.CredentialProfileView{ID: 11, ConnectorKind: Kind},
		ActionName: ActionDescribeTable,
		Input: map[string]any{
			"schema": "public",
			"table":  "orders",
		},
	})
	if err != nil {
		t.Fatalf("prepare describe_table: %v", err)
	}
	if prepared.Payload["schema"] != "public" || prepared.Payload["table"] != "orders" {
		t.Fatalf("unexpected payload: %#v", prepared.Payload)
	}
}

func TestPrepareDescribeTableRequiresTable(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
		ActionName: ActionDescribeTable,
		Input:      map[string]any{"table": " "},
	})
	if err == nil {
		t.Fatal("expected missing table error")
	}
}

func TestPrepareReadonlyQuery(t *testing.T) {
	connector := New()
	request := connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", Name: "main-db", ConnectorKind: Kind, Config: map[string]any{"connection_mode": "over_ssh", "transport_target_ref": "ssh:3:5"}},
		Profile:    connectors.CredentialProfileView{ID: 11, ConnectorKind: Kind},
		ActionName: ActionQueryReadonly,
		Input: map[string]any{
			"sql":      "-- smoke\nselect id, email from users where active = true;",
			"max_rows": float64(maxRows + 500),
		},
		Reason: "inspect active users",
	}
	connectortest.AssertPrepareActionDeterministic(t, connector, request)
	prepared, err := connector.PrepareAction(context.Background(), request)
	if err != nil {
		t.Fatalf("prepare query_readonly: %v", err)
	}
	if prepared.Payload["max_rows"] != maxRows {
		t.Fatalf("max_rows = %#v", prepared.Payload["max_rows"])
	}
	if prepared.ContextMaterial["reason"] != "inspect active users" {
		t.Fatalf("reason missing from context material: %#v", prepared.ContextMaterial)
	}
	if prepared.ContextMaterial["connection_mode"] != "over_ssh" || prepared.ContextMaterial["transport_target_ref"] != "ssh:3:5" {
		t.Fatalf("transport context missing from context material: %#v", prepared.ContextMaterial)
	}
}

func TestPrepareReadonlyQueryRejectsUnsafeSQL(t *testing.T) {
	for _, sql := range []string{
		"",
		"update users set admin = true",
		"select 1; drop table users",
		"select * into temp exported_users from users",
		"with deleted as (delete from users returning *) select * from deleted",
		"listen events",
		"notify events, 'changed'",
		"set statement_timeout = 0",
		"execute prepared_query",
		"select pg_notify('events', 'changed')",
		"select count(pg_notify('events', 'changed'))",
		"with active_users(id) as (select pg_notify('events', 'changed')) select id from active_users",
		"select row_data.id from (select pg_notify('events', 'changed')) as row_data(id)",
		"explain (format json, costs off) select pg_notify('events', 'changed')",
		"select lower(pg_read_file('/etc/passwd'))",
		"select a\u0301()",
		"select dblink_exec('dbname=other', 'delete from users')",
		"select lo_export(123, '/tmp/export')",
		"select public.custom_read_function()",
		"select public.select()",
		"select audit.where()",
		`select "pg_notify"('events', 'changed')`,
		`select * from json_to_record('{"values":"probe"}') as (values public.review_domain)`,
	} {
		_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
			Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
			ActionName: ActionQueryReadonly,
			Input:      map[string]any{"sql": sql},
		})
		if err == nil {
			t.Fatalf("expected %q to be rejected", sql)
		}
	}
}

func TestPrepareReadonlyQueryAcceptsApprovedPostgresFunctions(t *testing.T) {
	for _, sql := range []string{
		"select count(*), max(created_at) from users",
		"select pg_catalog.lower(email) from users",
		"select jsonb_build_object('id', id) from users",
		"with active_users(id) as (select id from users) select id from active_users",
		"explain (format json, costs off) select id from users",
		"select row_data.id from (values (1)) as row_data(id)",
		"select row_data.id from (values (1)) row_data(id)",
	} {
		_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
			Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
			ActionName: ActionQueryReadonly,
			Input:      map[string]any{"sql": sql},
		})
		if err != nil {
			t.Fatalf("expected %q to be accepted: %v", sql, err)
		}
	}
}

func TestProvisionScopeInputSupportsNestedSelection(t *testing.T) {
	scope, err := provisionScopeInput(map[string]any{
		"scope": map[string]any{
			"schemas": []any{
				map[string]any{
					"schema":     "public",
					"all_tables": false,
					"tables": []any{
						map[string]any{"table": "orders", "all_columns": true},
						map[string]any{"table": "users", "columns": []any{"id", "email"}},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("scope input: %v", err)
	}
	if scope.AllSchemas || len(scope.Schemas) != 1 || len(scope.Schemas[0].Tables) != 2 {
		t.Fatalf("unexpected scope: %#v", scope)
	}
	if !scope.Schemas[0].Tables[0].AllColumns || len(scope.Schemas[0].Tables[1].Columns) != 2 {
		t.Fatalf("unexpected tables: %#v", scope.Schemas[0].Tables)
	}
}

func TestProvisionScopeInputRejectsUnsafeSelection(t *testing.T) {
	for _, input := range []map[string]any{
		{"scope": map[string]any{"schemas": []any{map[string]any{"schema": "bad-name", "all_tables": true}}}},
		{"scope": map[string]any{"schemas": []any{map[string]any{"schema": "public", "tables": []any{map[string]any{"table": "orders;drop", "all_columns": true}}}}}},
		{"scope": map[string]any{"schemas": []any{map[string]any{"schema": "public", "tables": []any{map[string]any{"table": "orders", "columns": []any{"bad-name"}}}}}}},
	} {
		if _, err := provisionScopeInput(input); err == nil {
			t.Fatalf("expected unsafe scope to be rejected: %#v", input)
		}
	}
}

func TestProvisionRoleStatementsBuildsScopedGrants(t *testing.T) {
	scope := provisionScope{
		Schemas: []provisionSchemaScope{
			{
				Schema: "public",
				Tables: []provisionTableScope{
					{Table: "orders", AllColumns: true},
					{Table: "users", Columns: []string{"id", "email"}},
				},
			},
		},
	}
	statements, summary, err := provisionRoleStatements(
		connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{"database": "appdb"}},
		"app_reader",
		"secret-value",
		"read_only",
		scope,
	)
	if err != nil {
		t.Fatalf("role statements: %v", err)
	}
	joined := strings.Join(statements, "\n")
	for _, want := range []string{
		`CREATE ROLE "app_reader" LOGIN PASSWORD 'secret-value'`,
		`GRANT CONNECT ON DATABASE "appdb" TO "app_reader"`,
		`GRANT SELECT ON TABLE "public"."orders" TO "app_reader"`,
		`GRANT SELECT ("id", "email") ON TABLE "public"."users" TO "app_reader"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("statements missing %q in:\n%s", want, joined)
		}
	}
	grants, ok := summary["grants"].([]map[string]any)
	if !ok || len(grants) != 2 {
		t.Fatalf("unexpected summary grants: %#v", summary)
	}
}

func TestProvisionRoleStatementsRejectsColumnScopedWrites(t *testing.T) {
	_, _, err := provisionRoleStatements(
		connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{"database": "appdb"}},
		"app_writer",
		"secret-value",
		"read_write",
		provisionScope{Schemas: []provisionSchemaScope{{Schema: "public", Tables: []provisionTableScope{{Table: "users", Columns: []string{"email"}}}}}},
	)
	if err == nil {
		t.Fatal("expected column-scoped write grants to be rejected")
	}
}

func TestProvisionRoleStatementsGrantOnlyOwnedSequencesForWritableScopes(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		scope     provisionScope
		wantScope string
	}{
		{
			name:      "all user schemas",
			scope:     provisionScope{AllSchemas: true},
			wantScope: "ns.nspname NOT LIKE 'pg_%' AND ns.nspname <> 'information_schema'",
		},
		{
			name:      "one schema",
			scope:     provisionScope{Schemas: []provisionSchemaScope{{Schema: "public", AllTables: true}}},
			wantScope: "ns.nspname = 'public'",
		},
		{
			name: "one table",
			scope: provisionScope{Schemas: []provisionSchemaScope{{
				Schema: "public", Tables: []provisionTableScope{{Table: "orders", AllColumns: true}},
			}}},
			wantScope: "ns.nspname = 'public' AND tbl.relname = 'orders'",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			statements, summary, err := provisionRoleStatements(
				connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{"database": "appdb"}},
				"app_writer", "secret-value", "read_write", testCase.scope,
			)
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(statements, "\n")
			for _, want := range []string{
				"pg_get_serial_sequence",
				"GRANT USAGE ON SEQUENCE %s TO %I",
				testCase.wantScope,
			} {
				if !strings.Contains(joined, want) {
					t.Fatalf("statements missing %q:\n%s", want, joined)
				}
			}
			if strings.Contains(joined, "ON ALL SEQUENCES") {
				t.Fatalf("writable scope granted unrelated sequences:\n%s", joined)
			}
			if summary["sequence_privileges"] != "usage_on_sequences_owned_by_writable_tables" {
				t.Fatalf("summary = %#v", summary)
			}
		})
	}
}

func TestProvisionRoleStatementsDoNotGrantSequencesToReadonlyRoles(t *testing.T) {
	statements, summary, err := provisionRoleStatements(
		connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{"database": "appdb"}},
		"app_reader", "secret-value", "read_only", provisionScope{AllSchemas: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(statements, "\n"); strings.Contains(joined, "GRANT USAGE ON SEQUENCE") {
		t.Fatalf("read-only role received sequence usage:\n%s", joined)
	}
	if _, ok := summary["sequence_privileges"]; ok {
		t.Fatalf("read-only summary advertises sequence privileges: %#v", summary)
	}
}

func TestPrepareReadonlyQueryIgnoresUnsafeWordsInsideNonCodeSQL(t *testing.T) {
	for _, sql := range []string{
		"select 'drop table users; update accounts' as message",
		`select "drop" from "update"`,
		"select 1 -- drop table users;\n",
		"select /* alter table users */ 1",
		"select $$delete from users;$$ as sample",
		"with sample as (select 'truncate table x' as text) select * from sample",
	} {
		_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
			Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
			ActionName: ActionQueryReadonly,
			Input:      map[string]any{"sql": sql},
		})
		if err != nil {
			t.Fatalf("expected %q to be accepted, got %v", sql, err)
		}
	}
}

func TestPrepareUnsupportedAction(t *testing.T) {
	_, err := New().PrepareAction(context.Background(), connectors.ActionRequest{
		Target:     connectors.TargetView{Ref: "postgres:7:11", ConnectorKind: Kind},
		ActionName: "vacuum",
	})
	if !errors.Is(err, ErrUnsupportedAction) {
		t.Fatalf("expected ErrUnsupportedAction, got %v", err)
	}
}

func TestExecuteActionRequiresNetworkTransport(t *testing.T) {
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: connectors.TargetView{
			ConnectorKind: Kind,
			Config: map[string]any{
				"connection_mode":      "over_ssh",
				"transport_target_ref": "ssh:3:5",
				"host":                 "127.0.0.1",
				"port":                 5432,
				"database":             "app",
			},
		},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "app"}},
		Secrets: fakeSecrets{"password": "secret"},
	}, connectors.PreparedAction{ActionName: ActionQueryReadonly})
	if !errors.Is(err, ErrMissingTransport) {
		t.Fatalf("expected ErrMissingTransport, got %v", err)
	}
}

func TestExecuteActionRequiresCredentialSecrets(t *testing.T) {
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: connectors.TargetView{
			ConnectorKind: Kind,
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            "127.0.0.1",
				"port":            5432,
				"database":        "app",
			},
		},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "app"}},
		Secrets: fakeSecrets{},
	}, connectors.PreparedAction{ActionName: ActionQueryReadonly, Payload: map[string]any{"sql": "select 1"}})
	if !errors.Is(err, ErrMissingSecret) {
		t.Fatalf("expected ErrMissingSecret, got %v", err)
	}
}

func TestConnectPreservesCredentialProviderFailure(t *testing.T) {
	providerErr := errors.New("vault lease expired")
	_, err := connect(context.Background(), connectors.RuntimeContext{
		Target: connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{
			"connection_mode": "direct", "host": "127.0.0.1", "port": 5432, "database": "app",
		}},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "app"}},
		Secrets: failingPostgresSecrets{err: providerErr},
	})
	if !errors.Is(err, providerErr) || !errors.Is(err, connectors.ErrSecretProvider) {
		t.Fatalf("connect error=%v, want provider failure", err)
	}
	if status := classifyTestError(err); status != connectors.TestUnknownError {
		t.Fatalf("provider failure status=%s, want unknown_error", status)
	}
}

func TestExecuteActionValidatesTargetConfigBeforeDial(t *testing.T) {
	_, err := New().ExecuteAction(context.Background(), connectors.RuntimeContext{
		Target: connectors.TargetView{
			ConnectorKind: Kind,
			Config:        map[string]any{"connection_mode": "direct", "database": "app"},
		},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "app"}},
		Secrets: fakeSecrets{"password": "secret"},
	}, connectors.PreparedAction{ActionName: ActionQueryReadonly, Payload: map[string]any{"sql": "select 1"}})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestRestoreRejectsInvalidStreamsBeforeConnecting(t *testing.T) {
	connector := New()
	tests := []struct {
		name    string
		request connectors.RestoreRequest
		want    string
	}{
		{name: "missing stream", request: connectors.RestoreRequest{}, want: "empty"},
		{
			name: "oversized stream",
			request: connectors.RestoreRequest{
				Content: bytes.NewReader([]byte("select 1;")),
				Size:    maxBackupBytes + 1,
			},
			want: "too large",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := connector.Restore(context.Background(), connectors.RuntimeContext{}, test.request)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Restore() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRestoreTreatsEveryPostDispatchPSQLFailureAsOutcomeUnknown(t *testing.T) {
	err := restoreWithFakePSQL(t, "echo 'ERROR: relation missing' >&2\nexit 3", context.Background())
	if err == nil || !strings.Contains(err.Error(), "relation missing") {
		t.Fatalf("restore error = %v", err)
	}
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("post-dispatch SQL failure status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
}

func TestRestoreTreatsProcessStartFailureAsDefinite(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := New().Restore(context.Background(), postgresRestoreTestRuntime(), connectors.RestoreRequest{
		Filename: "restore.sql", Content: strings.NewReader("select 1;"), Size: 9,
	})
	if err == nil || connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		t.Fatalf("process start failure must remain definite: %v", err)
	}
}

func TestRestoreTreatsPostStartAuthenticationFailureAsOutcomeUnknown(t *testing.T) {
	err := restoreWithFakePSQL(t, "echo 'FATAL: password authentication failed for user app' >&2\nexit 2", context.Background())
	if err == nil || connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("post-start authentication status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
}

func TestRestoreTreatsPostDispatchConnectionLossAsOutcomeUnknown(t *testing.T) {
	err := restoreWithFakePSQL(t, "echo 'server closed the connection unexpectedly' >&2\nexit 2", context.Background())
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("connection loss status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
	details := connectors.ErrorDetails(err)
	if details["retry_safe"] != false || details["dispatch_stage"] != "process_observation" {
		t.Fatalf("unexpected restore outcome details: %#v", details)
	}
}

func TestRestoreTreatsPostStartCancellationAsOutcomeUnknown(t *testing.T) {
	directory := t.TempDir()
	started := filepath.Join(directory, "started")
	installFakePSQL(t, directory, "printf started > "+started+"\nexec sleep 5")
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- restoreWithInstalledFakePSQL(t, ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for !fileExists(started) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !fileExists(started) {
		cancel()
		t.Fatal("fake psql did not start")
	}
	cancel()
	err := <-result
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("canceled restore status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
}

func TestRestoreRequiresFullInputAcknowledgement(t *testing.T) {
	err := restoreWithFakePSQL(t, "exit 0", context.Background())
	if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown {
		t.Fatalf("early psql exit status = %q, want outcome_unknown: %v", connectors.ErrorStatus(err), err)
	}
}

func TestRestoreDisablesPSQLRCAndConsumesCompletionMarker(t *testing.T) {
	script := `
case " $* " in
  *" --no-psqlrc "*) ;;
  *) echo "missing --no-psqlrc" >&2; exit 2 ;;
esac
while IFS= read -r line; do
  case "$line" in
    "\\echo "*) printf '%s\n' "${line#\\echo }" ;;
  esac
done`
	directory := t.TempDir()
	installFakePSQL(t, directory, script)
	result, err := New().Restore(context.Background(), postgresRestoreTestRuntime(), connectors.RestoreRequest{
		Filename: "restore.sql", Content: strings.NewReader("select 1;"), Size: 9,
	})
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("Restore() result=%#v err=%v", result, err)
	}
	output, _ := result.Output.(map[string]any)
	if stdout, _ := output["stdout"].(string); strings.Contains(stdout, "aipermission_restore_complete_") {
		t.Fatalf("internal completion marker leaked in output: %q", stdout)
	}
}

func TestRestoreRejectsUnsafePSQLMetaCommandsBeforeDispatch(t *testing.T) {
	for _, command := range []string{
		`\set ON_ERROR_STOP off`, `\quit`, `\include secrets.sql`,
		`select 1 \gexec`, `select 1; \! id`,
		"\\restrict `touch /tmp/aipermission-restore-rce; printf token`",
		`\restrict token extra`, "\\restrict\ttoken", `\unrestrict token`,
		"\\restrict token\n\\unrestrict other",
		"\\restrict token\n\\unrestrict token\n\\restrict token",
		"SELECT U&\"unsafe\\0061\";\n\\echo must-not-run",
		"SELECT 'unterminated",
		"SELECT \"unterminated",
		"SELECT $tag$unterminated",
		"SELECT 1; /* unterminated",
	} {
		t.Run(command, func(t *testing.T) {
			directory := t.TempDir()
			started := filepath.Join(directory, "started")
			installFakePSQL(t, directory, "printf started > "+started)
			content := "select 1;\n" + command + "\n"
			_, err := New().Restore(t.Context(), postgresRestoreTestRuntime(), connectors.RestoreRequest{
				Filename: "restore.sql", Content: strings.NewReader(content), Size: int64(len(content)),
			})
			if err == nil || fileExists(started) {
				t.Fatalf("unsafe command error=%v dispatched=%v", err, fileExists(started))
			}
		})
	}
}

func TestRestoreReportsExplicitTransactionControlAsOutcomeUnknown(t *testing.T) {
	script := `
while IFS= read -r line; do
  case "$line" in
    "\\echo "*) printf '%s\n' "${line#\\echo }" ;;
  esac
done`
	directory := t.TempDir()
	installFakePSQL(t, directory, script)
	for _, statement := range []string{
		"BEGIN; SELECT 1; COMMIT;",
		"START TRANSACTION; SELECT 1;",
		"SELECT 1; ROLLBACK;",
		"CREATE TABLE prepared_restore (id integer); PREPARE TRANSACTION 'restore-test';",
		"/* outer /* inner */ still outer */ ROLLBACK;",
	} {
		result, err := New().Restore(t.Context(), postgresRestoreTestRuntime(), connectors.RestoreRequest{
			Filename: "restore.sql", Content: strings.NewReader(statement), Size: int64(len(statement)),
		})
		if err != nil || result.Status != connectors.ResultOutcomeUnknown || result.Metadata["reason"] != "restore_artifact_controls_transaction" {
			t.Fatalf("statement=%q result=%#v err=%v", statement, result, err)
		}
	}
}

func TestRestoreIgnoresTransactionWordsInsideDataAndComments(t *testing.T) {
	content := "SELECT 'ROLLBACK', $$COMMIT$$, \"BEGIN\"; -- ABORT\n/* outer /* START TRANSACTION */ PREPARE TRANSACTION */ PREPARE query_plan AS SELECT 1;"
	_, cleanup, controlsTransaction, err := validatedPostgresRestoreContent(t.Context(), connectors.RestoreRequest{
		Content: strings.NewReader(content), Size: int64(len(content)),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if controlsTransaction {
		t.Fatal("transaction words inside strings, identifiers, or comments must not affect restore outcome")
	}
}

func TestRestoreValidatesExactArtifactSizeBeforeDispatch(t *testing.T) {
	for _, size := range []int64{8, 10} {
		directory := t.TempDir()
		started := filepath.Join(directory, "started")
		installFakePSQL(t, directory, "printf started > "+started)
		_, err := New().Restore(t.Context(), postgresRestoreTestRuntime(), connectors.RestoreRequest{
			Filename: "restore.sql", Content: strings.NewReader("select 1;"), Size: size,
		})
		if err == nil || fileExists(started) {
			t.Fatalf("size=%d error=%v dispatched=%v", size, err, fileExists(started))
		}
	}
}

func TestRestoreAllowsPgDumpRestrictAndCopyDataMarkers(t *testing.T) {
	content := "\\restrict token\n" +
		"SELECT E'C:\\\\data', '\\\\literal'; -- \\ignored comment\n" +
		"SELECT $$\\dollar quote$$; /* \\block comment */\n" +
		"COPY public.items (value) FROM stdin;\n\\N\n\\.\n\\unrestrict token\n"
	_, cleanup, _, err := validatedPostgresRestoreContent(t.Context(), connectors.RestoreRequest{
		Content: strings.NewReader(content), Size: int64(len(content)),
	})
	if err != nil {
		t.Fatalf("valid pg_dump content rejected: %v", err)
	}
	cleanup()
}

func TestRestoreStagesAndRemovesNonSeekableContent(t *testing.T) {
	content := "select 1;\n"
	reader, cleanup, _, err := validatedPostgresRestoreContent(t.Context(), connectors.RestoreRequest{
		Content: io.LimitReader(strings.NewReader(content), int64(len(content))), Size: int64(len(content)),
	})
	if err != nil {
		t.Fatal(err)
	}
	staged, ok := reader.(*os.File)
	if !ok {
		t.Fatalf("non-seekable restore reader = %T, want staged file", reader)
	}
	name := staged.Name()
	cleanup()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("staged restore artifact still exists after cleanup: %v", err)
	}
}

func TestRestoreValidationHonorsCancellationBeforeDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, cleanup, _, err := validatedPostgresRestoreContent(ctx, connectors.RestoreRequest{
		Content: strings.NewReader("select 1;\n"), Size: int64(len("select 1;\n")),
	})
	if cleanup != nil {
		cleanup()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation error = %v", err)
	}
}

func restoreWithFakePSQL(t *testing.T, script string, ctx context.Context) error {
	t.Helper()
	directory := t.TempDir()
	installFakePSQL(t, directory, script)
	return restoreWithInstalledFakePSQL(t, ctx)
}

func installFakePSQL(t *testing.T, directory, script string) {
	t.Helper()
	psql := filepath.Join(directory, "psql")
	if err := os.WriteFile(psql, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func restoreWithInstalledFakePSQL(t *testing.T, ctx context.Context) error {
	t.Helper()
	_, err := New().Restore(ctx, postgresRestoreTestRuntime(), connectors.RestoreRequest{
		Filename: "restore.sql", Content: strings.NewReader("select 1;"), Size: 9,
	})
	return err
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func postgresRestoreTestRuntime() connectors.RuntimeContext {
	return connectors.RuntimeContext{
		Target: connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{
			"connection_mode": "direct", "host": "127.0.0.1", "port": 5432, "database": "app",
		}},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "app"}},
		Secrets: fakeSecrets{"password": "secret"},
	}
}

func TestPublicStringMissingKeyIsEmpty(t *testing.T) {
	if got := publicString(map[string]any{"username": " app "}, "username"); got != "app" {
		t.Fatalf("username = %q", got)
	}
	if got := publicString(map[string]any{"username": "app"}, "missing"); got != "" {
		t.Fatalf("missing public value should be empty, got %q", got)
	}
	if got := publicString(map[string]any{"username": nil}, "username"); got != "" {
		t.Fatalf("nil public value should be empty, got %q", got)
	}
}

func withAction(req connectors.ActionRequest, actionName string, input map[string]any) connectors.ActionRequest {
	req.ActionName = actionName
	req.Input = input
	return req
}

func hasField(schema connectors.Schema, name string) bool {
	for _, field := range schema.Fields {
		if field.Name == name {
			return true
		}
	}
	return false
}

func fieldOptionsContain(schema connectors.Schema, name string, value string) bool {
	for _, field := range schema.Fields {
		if field.Name != name {
			continue
		}
		for _, option := range field.Options {
			if option.Value == value {
				return true
			}
		}
	}
	return false
}

func defaultFieldValue(schema connectors.Schema, name string) any {
	for _, field := range schema.Fields {
		if field.Name == name {
			return field.Default
		}
	}
	return nil
}

type fakeSecrets map[string]string

func (s fakeSecrets) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := s[name]
	if !ok {
		return "", connectors.ErrSecretNotFound
	}
	return value, nil
}

type failingPostgresSecrets struct{ err error }

func (s failingPostgresSecrets) GetSecret(context.Context, string) (string, error) {
	return "", s.err
}

type fakePostgresCapabilities struct {
	transport connectors.NetworkTransport
}

func (capabilities fakePostgresCapabilities) RuntimeCapability(name string) connectors.RuntimeCapability {
	if name == connectors.NetworkTransportCapabilityName {
		return capabilities.transport
	}
	return nil
}

type noDialPostgresTransport struct{}

func (noDialPostgresTransport) ConnectorRuntimeCapability() string {
	return connectors.NetworkTransportCapabilityName
}

func (noDialPostgresTransport) DialConnectorTCP(context.Context, connectors.NetworkDialRequest) (net.Conn, error) {
	return nil, errors.New("unexpected test dial")
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func containsPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

func argumentValue(arguments []string, name string) string {
	for index := 0; index+1 < len(arguments); index++ {
		if arguments[index] == name {
			return arguments[index+1]
		}
	}
	return ""
}
