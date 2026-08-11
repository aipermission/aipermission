package conformance_test

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	clickhouseconnector "github.com/aipermission/aipermission/backend/internal/connectors/clickhouse"
)

func TestClickHouseRealService(t *testing.T) {
	requireConformance(t)
	connector := clickhouseconnector.New()
	runtime := connectors.RuntimeContext{
		Target: connectors.TargetView{
			ID: 5, Ref: "clickhouse:5:5", ConnectorKind: clickhouseconnector.Kind, Name: "conformance-clickhouse",
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            fixtureHost("AIPERMISSION_CLICKHOUSE_HOST", "127.0.0.1"),
				"port":            fixturePort(t, "AIPERMISSION_CLICKHOUSE_PORT", 9000),
				"database":        "aipermission",
				"tls_mode":        "disable",
			},
		},
		Profile: connectors.CredentialProfileView{
			ID: 5, TargetID: 5, ConnectorKind: clickhouseconnector.Kind, Kind: "username_password", Label: "conformance",
			Public: map[string]any{"username": "aipermission"},
		},
		Secrets:      fixtureSecrets{"password": "conformance-only"},
		Capabilities: fixtureCapabilities{},
	}

	assertConnection(t, connector, runtime)
	result := executeAction(t, connector, runtime, clickhouseconnector.ActionQueryReadonly, map[string]any{
		"sql":      "SELECT currentDatabase() AS database_name, 'clickhouse-conformance' AS marker",
		"max_rows": 5,
	})
	assertResultContains(t, result, "clickhouse-conformance")
}
