package postgresconnector

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if host := argumentValue(invocation.Args, "--host"); host != "db.internal.example" {
		t.Fatalf("CLI TLS host = %q, want original database host", host)
	}
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

func TestRestoreTreatsTransactionalPSQLFailureAsDefinite(t *testing.T) {
	directory := t.TempDir()
	psql := filepath.Join(directory, "psql")
	if err := os.WriteFile(psql, []byte("#!/bin/sh\necho 'ERROR: relation missing' >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	_, err := New().Restore(context.Background(), connectors.RuntimeContext{
		Target: connectors.TargetView{ConnectorKind: Kind, Config: map[string]any{
			"connection_mode": "direct", "host": "127.0.0.1", "port": 5432, "database": "app",
		}},
		Profile: connectors.CredentialProfileView{Public: map[string]any{"username": "app"}},
		Secrets: fakeSecrets{"password": "secret"},
	}, connectors.RestoreRequest{Filename: "restore.sql", Content: strings.NewReader("select 1;"), Size: 9})
	if err == nil || !strings.Contains(err.Error(), "relation missing") {
		t.Fatalf("restore error = %v", err)
	}
	if connectors.ErrorStatus(err) == connectors.ResultOutcomeUnknown {
		t.Fatalf("single-transaction psql failure was marked uncertain: %v", err)
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
