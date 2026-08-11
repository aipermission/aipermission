package conformance_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
)

func TestPostgresRealService(t *testing.T) {
	requireConformance(t)
	connector := postgresconnector.New()
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 1, Ref: "postgres:1:1", ConnectorKind: postgresconnector.Kind, Name: "conformance-postgres",
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            fixtureHost("AIPERMISSION_POSTGRES_HOST", "127.0.0.1"),
				"port":            fixturePort(t, "AIPERMISSION_POSTGRES_PORT", 5432),
				"database":        "aipermission",
				"ssl_mode":        "disable",
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 1, TargetID: 1, ConnectorKind: postgresconnector.Kind, Kind: "username_password", Label: "conformance",
			Public: map[string]any{"username": "aipermission"},
		},
		Secrets:      fixtureSecrets{"password": "conformance-only"},
		Capabilities: fixtureCapabilities{},
	}

	assertConnection(t, connector, runtime)
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql":      "select current_database() as database_name, 'postgres-conformance' as marker",
		"max_rows": 5,
	})
	assertResultContains(t, result, "postgres-conformance")
}
