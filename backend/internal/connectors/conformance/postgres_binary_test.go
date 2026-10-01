package conformance_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
)

const postgresBinaryFixtureSQL = `SELECT bytea '\xff0041' AS binary_value, bytea '\xefbfbd' AS replacement,
	bytea '' AS empty_value, CASE WHEN false THEN bytea '' ELSE NULL END AS null_value, 'text' AS text_value`

func TestPostgresBinaryFixtureReadOnlyPolicy(t *testing.T) {
	prepared, err := postgresconnector.New().PrepareAction(t.Context(), connectors.ActionRequest{
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": postgresBinaryFixtureSQL, "max_rows": 1},
	})
	if err != nil {
		t.Fatalf("real-service fixture is rejected before dispatch: %v", err)
	}
	if prepared.Payload["sql"] != postgresBinaryFixtureSQL {
		t.Fatal("real-service bytea fixture was changed by preparation")
	}
}

func assertPostgresBinaryResults(t *testing.T, connector connectors.Connector, runtime connectors.RuntimeContext) {
	t.Helper()
	result := executeAction(t, connector, runtime, postgresconnector.ActionQueryReadonly, map[string]any{
		"sql":      postgresBinaryFixtureSQL,
		"max_rows": 1,
	})
	encoded, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	var output struct {
		Rows      []map[string]any `json:"rows"`
		Truncated bool             `json:"truncated"`
	}
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{
		"binary_value": `\xff0041`, "replacement": `\xefbfbd`,
		"empty_value": `\x`, "null_value": nil, "text_value": "text",
	}}
	if output.Truncated || !reflect.DeepEqual(output.Rows, want) {
		t.Fatalf("real bytea query lost identity: %#v", output)
	}
}
