package conformance_test

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sqlserverconnector "github.com/aipermission/aipermission/backend/internal/connectors/sqlserver"
)

func assertSQLServerResultTypes(t *testing.T, runtime connectors.RuntimeContext) {
	t.Helper()
	connector := sqlserverconnector.New()
	for _, column := range []string{"xml_value", "variant_value", "spatial_value"} {
		prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile, ActionName: "query_readonly", Input: map[string]any{"sql": "SELECT 1 AS a, 2 AS b, " + column + " FROM dbo.result_types", "max_rows": 1}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := connector.ExecuteAction(t.Context(), runtime, prepared); err == nil || !strings.Contains(err.Error(), "CAST") {
			t.Fatalf("unsupported %s result not rejected: %v", column, err)
		}
	}
	result := executeAction(t, connector, runtime, "query_readonly", map[string]any{"sql": `SELECT date_value,time_value,datetime_value,smalldatetime_value,datetime2_value,offset_value,safe_alias,
 CAST(variant_value AS decimal(12,4)) AS amount,CAST(xml_value AS nvarchar(max)) AS xml_text FROM dbo.result_types`})
	row := result.Output.(map[string]any)["rows"].([]map[string]any)[0]
	for column, want := range map[string]any{
		"date_value": "2026-11-02", "time_value": "13:14:15.1234567", "datetime_value": "2026-11-02T13:14:15",
		"smalldatetime_value": "2026-11-02T13:14:00", "datetime2_value": "2026-11-02T13:14:15.1234567", "offset_value": "2026-11-02T13:14:15.1234567+05:30",
		"safe_alias": int64(17), "amount": "123.4500",
	} {
		if row[column] != want {
			t.Errorf("%s=%v want=%v", column, row[column], want)
		}
	}
	if result.Output.(map[string]any)["truncated"] != true {
		t.Fatal("CAST XML output did not report truncation")
	}
}
