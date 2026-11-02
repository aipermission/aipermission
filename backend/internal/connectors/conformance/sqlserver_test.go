package conformance_test

import (
	"context"
	"os"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sqlserverconnector "github.com/aipermission/aipermission/backend/internal/connectors/sqlserver"
)

func TestSQLServerRealService(t *testing.T) {
	requireConformance(t)
	certificate, err := os.ReadFile(fixtureHost("AIPERMISSION_SQLSERVER_CA", "/sqlserver-material/ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{ID: 11, Ref: "sqlserver:11:110", ConnectorKind: sqlserverconnector.Kind, Name: "fixture SQL Server", Config: map[string]any{
			"host": fixtureHost("AIPERMISSION_SQLSERVER_HOST", "sqlserver"), "port": fixturePort(t, "AIPERMISSION_SQLSERVER_PORT", 1433), "database": "aipermission", "tls_mode": "verify_full", "ca_certificate": string(certificate),
		}},
		Profile: connectors.CredentialProfileView{ID: 110, TargetID: 11, ConnectorKind: sqlserverconnector.Kind, Kind: "username_password", Label: "reader", Public: map[string]any{"username": "fixture_reader"}},
		Secrets: fixtureSecrets{"password": "Fixture-only-Password1!"}, Capabilities: fixtureCapabilities{},
	}
	connector := sqlserverconnector.New()
	assertConnection(t, connector, runtime)
	assertSQLServerPolicyCorpus(t, runtime, certificate)
	assertSQLServerResultTypes(t, runtime)
	quoted := executeAction(t, connector, runtime, "query_readonly", map[string]any{"sql": `SELECT "column""name" FROM dbo."quote""edge"`})
	if row := quoted.Output.(map[string]any)["rows"].([]map[string]any)[0]; row[`column"name`] != int64(17) {
		t.Fatalf("quoted identifier changed meaning: %#v", row)
	}
	for _, action := range []string{"get_schemas", "get_tables", "get_metadata"} {
		executeAction(t, connector, runtime, action, map[string]any{})
	}
	described := executeAction(t, connector, runtime, "describe_table", map[string]any{"schema": "dbo", "table": "order details"})
	rows := described.Output.(map[string]any)["rows"].([]map[string]any)
	if len(rows) != 2 || rows[0]["column_name"] != "second_column" || rows[1]["column_name"] != "id" {
		t.Fatalf("column ordinals changed: %#v", rows)
	}
	result := executeAction(t, connector, runtime, "query_readonly", map[string]any{"sql": "SELECT id,label,amount,payload FROM dbo.events ORDER BY id DESC", "max_rows": 1})
	output := result.Output.(map[string]any)
	rows = output["rows"].([]map[string]any)
	if output["truncated"] != true || len(rows) != 1 || rows[0]["id"] != "9223372036854775807" || rows[0]["amount"] != "12345678901234567890.123456" || rows[0]["payload"] != "0xff0012" {
		t.Fatalf("result contract: %#v", output)
	}
	for _, query := range []string{`SELECT * FROM [dbo].[order details]`, `SELECT * FROM [dbo].[name]]edge]`, `SELECT 1 AS "alias with spaces"`, `WITH c AS (SELECT id FROM dbo.events) SELECT TOP (1) id FROM c`} {
		executeAction(t, connector, runtime, "query_readonly", map[string]any{"sql": query})
	}
	settings := executeAction(t, connector, runtime, "query_readonly", map[string]any{"sql": "SELECT @@LOCK_TIMEOUT AS lock_timeout, @@TEXTSIZE AS text_size"})
	setting := settings.Output.(map[string]any)["rows"].([]map[string]any)[0]
	if setting["lock_timeout"] != int64(5000) || setting["text_size"] != int64(131072) {
		t.Fatalf("session limits not applied: %#v", setting)
	}
	large := executeAction(t, connector, runtime, "query_readonly", map[string]any{"sql": "SELECT value FROM dbo.large_values"})
	if large.Output.(map[string]any)["truncated"] != true {
		t.Fatal("server-side UTF-16 LOB truncation was not reported")
	}
	for _, query := range []string{"DELETE FROM dbo.events", "SELECT * INTO dbo.copy FROM dbo.events", "SELECT NEXT VALUE FOR dbo.fixture_sequence", "SELECT dbo.unreviewed()"} {
		if _, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: "query_readonly", Input: map[string]any{"sql": query}}); err == nil {
			t.Fatalf("unsafe SQL accepted: %s", query)
		}
	}
	bad := runtime
	bad.Secrets = fixtureSecrets{"password": "not-the-fixture-password"}
	if result, err := connector.TestConnection(t.Context(), bad); err != nil || result.Status != connectors.TestFailedAuth {
		t.Fatalf("wrong password: %#v %v", result, err)
	}
	bad = runtime
	bad.Target.Config = cloneSQLConfig(runtime.Target.Config)
	bad.Target.Config["tls_server_name"] = "wrong.example.test"
	if result, err := connector.TestConnection(t.Context(), bad); err != nil || result.Status != connectors.TestFailedTLS {
		t.Fatalf("hostname mismatch: %#v %v", result, err)
	}
	bad.Target.Config["tls_server_name"] = ""
	delete(bad.Target.Config, "ca_certificate")
	if result, err := connector.TestConnection(t.Context(), bad); err != nil || result.Status != connectors.TestFailedTLS {
		t.Fatalf("untrusted certificate: %#v %v", result, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if result, err := connector.TestConnection(ctx, runtime); err != nil || result.Status != connectors.TestFailedNetwork {
		t.Fatalf("cancellation: %#v %v", result, err)
	}
	assertSQLServerExecutingQueryCancellation(t, runtime, certificate)
}

func cloneSQLConfig(config map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range config {
		result[key] = value
	}
	return result
}
