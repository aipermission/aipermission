package conformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
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
	assertClickHouseSourceFunctionsStayReadonly(t, connector, runtime)
	for _, identifier := range []string{" events ", "`events`", `tenant\back`, "ending\\", "mixed\\`"} {
		quoted := "`" + strings.ReplaceAll(strings.ReplaceAll(identifier, "\\", "\\\\"), "`", "``") + "`"
		result := executeAction(t, connector, runtime, clickhouseconnector.ActionQueryReadonly, map[string]any{
			"sql": "SELECT 1 AS " + quoted, "max_rows": 1,
		})
		columns := result.Output.(map[string]any)["columns"].([]string)
		if len(columns) != 1 || columns[0] != identifier {
			t.Fatalf("native parser changed identifier %q: %#v", identifier, columns)
		}
	}
}

func assertClickHouseSourceFunctionsStayReadonly(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	for _, query := range []string{
		"SELECT * FROM url('http://127.0.0.1:1/read-only-canary', 'CSV', 'marker String')",
		"SELECT * FROM file('read-only-canary.csv', 'CSV', 'marker String')",
		"SELECT * FROM s3('http://127.0.0.1:1/read-only-canary', 'NOSIGN', 'CSV', 'marker String')",
		"SELECT getSetting('readonly') SETTINGS readonly=0",
	} {
		prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile,
			ActionName: clickhouseconnector.ActionQueryReadonly, Input: map[string]any{"sql": query, "max_rows": 1}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		_, err = connector.ExecuteAction(ctx, runtime, prepared)
		cancel()
		var exception *chdriver.Exception
		if !errors.As(err, &exception) || exception.Code != 164 {
			t.Fatalf("source escaped native readonly guard: query=%s err=%v", query, err)
		}
	}
}
