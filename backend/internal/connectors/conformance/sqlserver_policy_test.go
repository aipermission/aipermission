package conformance_test

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sqlserverconnector "github.com/aipermission/aipermission/backend/internal/connectors/sqlserver"
	mssql "github.com/microsoft/go-mssqldb"
	"github.com/microsoft/go-mssqldb/msdsn"
)

func assertSQLServerPolicyCorpus(t *testing.T, runtime connectors.RuntimeContext, certificate []byte) {
	t.Helper()
	data, err := os.ReadFile("../sqlserver/testdata/readonly-attacks.json")
	if err != nil {
		t.Fatal(err)
	}
	var queries []string
	if err := json.Unmarshal(data, &queries); err != nil {
		t.Fatal(err)
	}
	writer := runtime
	writer.Profile.Public = map[string]any{"username": "fixture_policy_writer"}
	writer.Secrets = fixtureSecrets{"password": "Fixture-only-Policy1!"}
	connector := sqlserverconnector.New()
	assertConnection(t, connector, writer)
	db := sqlServerFixtureDB(t, runtime, certificate, "fixture_policy_writer", "Fixture-only-Policy1!")
	defer db.Close()
	for _, query := range queries {
		t.Run("policy/"+query, func(t *testing.T) {
			// This login genuinely can write to this one disposable table. DENY
			// cannot hide a failed read-policy test as it could with the reader.
			if _, err := db.ExecContext(t.Context(), "DELETE FROM dbo.policy_canary; INSERT dbo.policy_canary VALUES (1)"); err != nil {
				t.Fatal(err)
			}
			_, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: writer.Target, Profile: writer.Profile, ActionName: "query_readonly", Input: map[string]any{"sql": query}})
			if err == nil {
				t.Error("unsafe query passed preparation")
			}
			prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: writer.Target, Profile: writer.Profile, ActionName: "query_readonly", Input: map[string]any{"sql": "SELECT 1"}})
			if err != nil {
				t.Fatal(err)
			}
			prepared.Payload["sql"] = query
			if result, err := connector.ExecuteAction(t.Context(), writer, prepared); err == nil || result.Status == connectors.ResultCompleted {
				t.Error("unsafe query passed execution revalidation")
			}
			var count, value int
			if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*),COALESCE(SUM(value),0) FROM dbo.policy_canary").Scan(&count, &value); err != nil {
				t.Fatal(err)
			}
			if count != 1 || value != 1 {
				t.Fatalf("read action mutated owned canary: count=%d value=%d", count, value)
			}
		})
	}
	for _, query := range []string{
		"SELECT 'a''b; DELETE dbo.policy_canary' AS [message]",
		"SELECT N'雪''b' AS [a]]b]",
		"SELECT value AS [SHUTDOWN] FROM dbo.policy_canary -- valid LF comment\n",
		"SELECT value\r\nFROM dbo.policy_canary",
		"SELECT DATEADD(day,1,CAST('2026-01-01' AS date)) AS next_day",
	} {
		executeAction(t, connector, writer, "query_readonly", map[string]any{"sql": query})
	}
}

func sqlServerFixtureDB(t *testing.T, runtime connectors.RuntimeContext, certificate []byte, username, password string) *sql.DB {
	t.Helper()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		t.Fatal("invalid fixture CA")
	}
	driver := mssql.NewConnectorConfig(msdsn.Config{
		Host: runtime.Target.Config["host"].(string), Port: uint64(runtime.Target.Config["port"].(int)), Database: "aipermission",
		User: username, Password: password, Encryption: msdsn.EncryptionRequired,
		TLSConfig:                 &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: runtime.Target.Config["host"].(string)},
		HostInCertificateProvided: true, DisableRetry: true, Protocols: []string{"tcp"}, PacketSize: 4096,
		ConnTimeout: 10 * time.Second, DialTimeout: 10 * time.Second,
	})
	db := sql.OpenDB(driver)
	db.SetMaxOpenConns(1)
	return db
}
