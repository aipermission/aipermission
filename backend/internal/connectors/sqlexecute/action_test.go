package sqlexecute

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func TestExplicitWriteContractAndNormalizedLimits(t *testing.T) {
	definition := Definition("fixture SQL")
	if err := connectors.ValidateActionDefinitions([]connectors.ActionDefinition{definition}, "fixture"); err != nil {
		t.Fatal(err)
	}
	if definition.Risk != connectors.RiskWrite || connectors.EffectiveRetryPolicy(definition).Class != connectors.RetryNonIdempotent {
		t.Fatal("SQL execution received read-only or retry-safe authority")
	}
	for _, sql := range []string{"SELECT 1", "INSERT INTO t VALUES (1); UPDATE t SET id=2", "CREATE TABLE t (id int)", "EXEC dbo.fixture"} {
		prepared, err := Prepare(connectors.ActionRequest{ActionName: Action, Input: map[string]any{"sql": sql}, Target: connectors.TargetView{Ref: "fixture:1:2"}, Profile: connectors.CredentialProfileView{ID: 2}}, "fixture", "fixture SQL")
		if err != nil || prepared.Risk != connectors.RiskWrite || prepared.Payload["sql"] != sql || prepared.Preview["sql"] != sql || prepared.TargetRef != "fixture:1:2" || prepared.ProfileID != 2 {
			t.Fatalf("write preparation changed its immutable authority: %#v %v", prepared, err)
		}
		if rows, seconds, err := Limits(prepared.Payload); err != nil || rows != 100 || seconds != 30 {
			t.Fatalf("default limits: %d %d %v", rows, seconds, err)
		}
	}
}

func TestExecutionInputsFailClosed(t *testing.T) {
	for _, input := range []map[string]any{
		{}, {"sql": " "}, {"sql": "SELECT\x00 1"}, {"sql": string([]byte{255})}, {"sql": strings.Repeat("x", MaxSQLBytes+1)},
		{"sql": "SELECT 1", "max_rows": 0}, {"sql": "SELECT 1", "max_rows": 1001}, {"sql": "SELECT 1", "max_rows": 1.2},
		{"sql": "SELECT 1", "timeout_seconds": 0}, {"sql": "SELECT 1", "timeout_seconds": 301},
		{"sql": "SELECT 1", "timeout_seconds": 1.2}, {"sql": "SELECT 1", "password": "not-allowed"},
	} {
		if _, err := Prepare(connectors.ActionRequest{ActionName: Action, Input: input}, "fixture", "fixture SQL"); err == nil {
			t.Fatalf("accepted invalid SQL execution input: %#v", input)
		}
	}
	if _, err := Prepare(connectors.ActionRequest{ActionName: "query_readonly", Input: map[string]any{"sql": "SELECT 1"}}, "fixture", "fixture"); err == nil {
		t.Fatal("read action crossed into write preparation")
	}
}
