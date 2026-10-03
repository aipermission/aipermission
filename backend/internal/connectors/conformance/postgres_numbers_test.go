package conformance_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
)

func assertPostgresNumericResult(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql": `SELECT 9007199254740993 AS big, -9223372036854775808 AS min,
			0.10000000000000000001 AS decimal, 42 AS safe, 0.1 AS fraction`, "max_rows": 1,
	})
	output, err := actionresult.Canonicalize(result.Output, actionresult.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var transported struct {
		Rows []map[string]any `json:"rows"`
	}
	if err := json.Unmarshal(encoded, &transported); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"big": "9007199254740993", "min": "-9223372036854775808", "decimal": "0.10000000000000000001", "safe": float64(42), "fraction": 0.1}}
	if !reflect.DeepEqual(transported.Rows, want) {
		t.Fatalf("real PostgreSQL numbers changed at the public boundary: %#v", transported.Rows)
	}
}
