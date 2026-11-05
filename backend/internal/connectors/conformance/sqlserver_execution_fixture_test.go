package conformance_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sqlserverconnector "github.com/aipermission/aipermission/backend/internal/connectors/sqlserver"
)

type sqlServerExecutionFixture struct {
	sqlExecutionFixture
	observer    *sql.DB
	certificate []byte
}

func newSQLServerExecutionFixture(t *testing.T) sqlServerExecutionFixture {
	t.Helper()
	requireConformance(t)
	certificate, err := os.ReadFile(fixtureHost("AIPERMISSION_SQLSERVER_CA", "/sqlserver-material/ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{ID: 11, Ref: "sqlserver:11:111", ConnectorKind: sqlserverconnector.Kind, Name: "SQL execution fixture",
			Config: map[string]any{"host": fixtureHost("AIPERMISSION_SQLSERVER_HOST", "sqlserver"),
				"port": fixturePort(t, "AIPERMISSION_SQLSERVER_PORT", 1433), "database": "aipermission",
				"tls_mode": "verify_full", "ca_certificate": string(certificate)}},
		Profile: connectors.CredentialProfileView{ID: 111, TargetID: 11, ConnectorKind: sqlserverconnector.Kind,
			Kind: "username_password", Label: "fixture executor", Public: map[string]any{"username": "fixture_executor"}},
		Secrets: fixtureSecrets{"password": "Fixture-only-Executor1!"}, Capabilities: fixtureCapabilities{},
	}
	name := sqlExecutionName()
	fixture := sqlServerExecutionFixture{
		sqlExecutionFixture: sqlExecutionFixture{connector: sqlserverconnector.New(), runtime: runtime,
			table: "dbo.[" + name + "]", procedure: "dbo.[" + name + "_proc]"},
		observer: sqlServerFixtureDB(t, runtime, certificate, "fixture_executor", "Fixture-only-Executor1!"), certificate: certificate,
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		defer fixture.observer.Close()
		for _, statement := range []string{"SET LOCK_TIMEOUT 1000; DROP PROCEDURE IF EXISTS " + fixture.procedure, "DROP TABLE IF EXISTS " + fixture.table} {
			if _, err := fixture.observer.ExecContext(ctx, statement); err != nil {
				t.Errorf("cleanup owned SQL execution fixture: %v", err)
			}
		}
	})
	var sysadmin, owner int
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if err := fixture.observer.QueryRowContext(ctx, "SELECT IS_SRVROLEMEMBER('sysadmin'), IS_ROLEMEMBER('db_owner')").Scan(&sysadmin, &owner); err != nil || sysadmin != 0 || owner != 0 {
		t.Fatalf("executor must not be sa/db_owner: sysadmin=%d owner=%d err=%v", sysadmin, owner, err)
	}
	runSQLExecution(t, fixture.sqlExecutionFixture,
		"CREATE TABLE "+fixture.table+" (id int PRIMARY KEY, value int NOT NULL); INSERT INTO "+fixture.table+" VALUES (1,0);", 100)
	return fixture
}

func (fixture sqlServerExecutionFixture) value(t *testing.T) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
	defer cancel()
	var value int
	if err := fixture.observer.QueryRowContext(ctx, "SELECT value FROM "+fixture.table+" WITH (READCOMMITTEDLOCK) WHERE id=1").Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func (fixture sqlServerExecutionFixture) readonly() sqlExecutionFixture {
	reader := fixture.sqlExecutionFixture
	reader.runtime.Profile = connectors.CredentialProfileView{ID: 110, TargetID: 11, ConnectorKind: sqlserverconnector.Kind,
		Kind: "username_password", Label: "fixture reader", Public: map[string]any{"username": "fixture_reader"}}
	reader.runtime.Target.Ref = connectors.FormatTargetRef(sqlserverconnector.Kind, 11, 110)
	reader.runtime.Secrets = fixtureSecrets{"password": "Fixture-only-Password1!"}
	return reader
}
