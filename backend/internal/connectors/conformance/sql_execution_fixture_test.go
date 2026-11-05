package conformance_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlresult"
)

type sqlExecutionFixture struct {
	connector connectors.Connector
	runtime   connectors.RuntimeContext
	table     string
	procedure string
}

type sqlExecutionOutput struct {
	ResultSets []sqlresult.Result `json:"result_sets"`
	Commands   []struct {
		Tag          string `json:"tag"`
		ReportedRows string `json:"reported_rows"`
	} `json:"commands"`
	Notices          []string `json:"notices"`
	CompletedBatches int      `json:"completed_batches"`
	RowCount         int      `json:"row_count"`
	Truncated        bool     `json:"truncated"`
}

func sqlExecutionName() string {
	return "ap_execute_" + strings.ToLower(rand.Text())
}

func connectorsSQLRequest(fixture sqlExecutionFixture, input map[string]any) connectors.ActionRequest {
	return connectors.ActionRequest{Target: fixture.runtime.Target, Profile: fixture.runtime.Profile,
		ActionName: "execute_sql", Input: input}
}

func prepareSQLExecution(t *testing.T, fixture sqlExecutionFixture, input map[string]any) connectors.PreparedAction {
	t.Helper()
	actions, err := fixture.connector.GetActionList(t.Context(), fixture.runtime.Target, fixture.runtime.Profile)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, action := range actions {
		if action.Name == "execute_sql" {
			found = true
			if action.Risk != connectors.RiskWrite || connectors.EffectiveRetryPolicy(action).Class != connectors.RetryNonIdempotent {
				t.Fatalf("execute_sql must be write/non_idempotent: %#v", action)
			}
		}
	}
	if !found {
		t.Fatal("connector does not advertise execute_sql")
	}
	prepared, err := fixture.connector.PrepareAction(t.Context(), connectors.ActionRequest{
		Source: "conformance", Target: fixture.runtime.Target, Profile: fixture.runtime.Profile,
		ActionName: "execute_sql", Input: input, Reason: "owned disposable SQL execution fixture",
	})
	if err != nil {
		t.Fatalf("prepare execute_sql: %v", err)
	}
	if prepared.Risk != connectors.RiskWrite || prepared.ActionName != "execute_sql" || prepared.Payload["sql"] != input["sql"] {
		t.Fatalf("prepared execution changed the approved script or risk: %#v", prepared)
	}
	assertPreparedActionExcludesSecrets(t, prepared, fixture.runtime.Secrets)
	return prepared
}

func runSQLExecution(t *testing.T, fixture sqlExecutionFixture, script string, maxRows int) sqlExecutionOutput {
	t.Helper()
	prepared := prepareSQLExecution(t, fixture, map[string]any{"sql": script, "max_rows": maxRows})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
	if err != nil || result.Status != connectors.ResultCompleted {
		t.Fatalf("execute_sql status=%s error=%v result error=%s output=%#v", result.Status, err, result.Error, result.Output)
	}
	return decodeSQLExecution(t, result)
}

func decodeSQLExecution(t *testing.T, result connectors.ActionResult) sqlExecutionOutput {
	t.Helper()
	encoded, err := json.Marshal(result.Output)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"result_sets", "commands", "notices", "completed_batches", "row_count", "truncated"} {
		if value, ok := fields[field]; !ok || string(value) == "null" {
			t.Fatalf("execution output missing non-null %s: %s", field, encoded)
		}
	}
	var output sqlExecutionOutput
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("execution output contract: %v: %s", err, encoded)
	}
	rows := 0
	for _, set := range output.ResultSets {
		rows += len(set.Rows)
		if set.RowCount != len(set.Rows) {
			t.Fatalf("result set row_count mismatch: %#v", set)
		}
	}
	if rows != output.RowCount {
		t.Fatalf("aggregate row_count=%d, actual=%d", output.RowCount, rows)
	}
	for _, command := range output.Commands {
		if _, err := strconv.ParseInt(command.ReportedRows, 10, 64); err != nil || command.Tag == "" {
			t.Fatalf("invalid command acknowledgement: %#v", command)
		}
	}
	return output
}

func assertSQLExecutionValue(t *testing.T, output sqlExecutionOutput, set int, column, want string) {
	t.Helper()
	if set >= len(output.ResultSets) || len(output.ResultSets[set].Rows) != 1 {
		t.Fatalf("expected one row in result set %d: %#v", set, output)
	}
	if value := fmt.Sprint(output.ResultSets[set].Rows[0][column]); value != want {
		t.Fatalf("result set %d %s=%s, want %s", set, column, value, want)
	}
}

func assertSQLExecutionUnknown(t *testing.T, result connectors.ActionResult, err error) {
	t.Helper()
	if err != nil {
		if connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "outcome_unknown" {
			t.Fatalf("post-dispatch error was not classified outcome_unknown: %v (%#v)", err, result)
		}
		return
	}
	if result.Status != connectors.ResultOutcomeUnknown || result.Error == "" {
		t.Fatalf("post-dispatch failure did not report outcome_unknown: %#v", result)
	}
	output, ok := result.Output.(map[string]any)
	if !ok || output["retry_safe"] != false {
		t.Fatalf("uncertain execution must prohibit replay: %#v", result)
	}
}

func assertSQLExecutionDenied(t *testing.T, fixture sqlExecutionFixture, script string) {
	t.Helper()
	prepared := prepareSQLExecution(t, fixture, map[string]any{"sql": script})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
	if result.Status == connectors.ResultCompleted || (err == nil && result.Status != connectors.ResultOutcomeUnknown) {
		t.Fatalf("readonly fixture did not reject dispatched write: %#v %v", result, err)
	}
	if err != nil && connectors.ErrorCode(err) == "" {
		t.Fatalf("permission error was not classified: %v", err)
	}
	message := strings.ToLower(result.Error + fmt.Sprint(err))
	if !strings.Contains(message, "permission") && !strings.Contains(message, "42501") {
		t.Fatalf("expected real database permission denial: %#v %v", result, err)
	}
}
