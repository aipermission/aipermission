package conformance_test

import (
	"fmt"
	"testing"
)

func assertSQLExecutionLifecycle(t *testing.T, fixture sqlExecutionFixture, separator string, batches int) {
	t.Helper()
	script := fmt.Sprintf(`ALTER TABLE %s ADD note varchar(100);%s
INSERT INTO %s (id,value,note) VALUES (2,2,'owned'),(3,3,'delete');
UPDATE %s SET value=7 WHERE id=2;
DELETE FROM %s WHERE id=3;
SELECT value,note FROM %s WHERE id=2;`, fixture.table, separator, fixture.table, fixture.table, fixture.table, fixture.table)
	output := runSQLExecution(t, fixture, script, 100)
	assertSQLExecutionValue(t, output, 0, "value", "7")
	assertSQLExecutionValue(t, output, 0, "note", "owned")
	if output.CompletedBatches != batches || len(output.Commands) < 4 || output.Truncated {
		t.Fatalf("lifecycle acknowledgement contract: %#v", output)
	}
	readback := executeAction(t, fixture.connector, fixture.runtime, "query_readonly", map[string]any{
		"sql": "SELECT value,note FROM " + fixture.table + " WHERE id=2",
	})
	assertResultContains(t, readback, "owned")
	runSQLExecution(t, fixture, "DROP TABLE "+fixture.table, 100)
}

func assertSQLExecutionMultipleResults(t *testing.T, fixture sqlExecutionFixture) {
	t.Helper()
	output := runSQLExecution(t, fixture, "SELECT 11 AS first_value; SELECT 22 AS second_value;", 100)
	if len(output.ResultSets) != 2 || output.RowCount != 2 || output.Truncated || output.CompletedBatches != 1 {
		t.Fatalf("multiple result set contract: %#v", output)
	}
	assertSQLExecutionValue(t, output, 0, "first_value", "11")
	assertSQLExecutionValue(t, output, 1, "second_value", "22")
}

func assertSQLExecutionRowCap(t *testing.T, fixture sqlExecutionFixture, separator string) {
	t.Helper()
	script := "SELECT 1 AS value UNION ALL SELECT 2 AS value; " +
		"UPDATE " + fixture.table + " SET value=value+1 WHERE id=1;" + separator +
		"SELECT 3 AS value; UPDATE " + fixture.table + " SET value=value+1 WHERE id=1;"
	output := runSQLExecution(t, fixture, script, 1)
	if !output.Truncated || output.RowCount != 1 {
		t.Fatalf("max_rows did not cap aggregate output: %#v", output)
	}
	readback := runSQLExecution(t, fixture, "SELECT value FROM "+fixture.table+" WHERE id=1", 100)
	assertSQLExecutionValue(t, readback, 0, "value", "2")
}

func assertSQLExecutionInputContract(t *testing.T, fixture sqlExecutionFixture) {
	t.Helper()
	prepared := prepareSQLExecution(t, fixture, map[string]any{"sql": "SELECT 1 AS value"})
	for key, want := range map[string]string{"max_rows": "100", "timeout_seconds": "30"} {
		if fmt.Sprint(prepared.Payload[key]) != want {
			t.Fatalf("%s default=%v, want %s", key, prepared.Payload[key], want)
		}
	}
	for _, input := range []map[string]any{
		{"sql": "SELECT 1", "max_rows": 0}, {"sql": "SELECT 1", "max_rows": 1001},
		{"sql": "SELECT 1", "timeout_seconds": 0}, {"sql": "SELECT 1", "timeout_seconds": 301},
	} {
		_, err := fixture.connector.PrepareAction(t.Context(), connectorsSQLRequest(fixture, input))
		if err == nil {
			t.Fatalf("invalid execution limits accepted: %#v", input)
		}
	}
	prepareSQLExecution(t, fixture, map[string]any{"sql": "SELECT 1", "max_rows": 1, "timeout_seconds": 1})
	prepareSQLExecution(t, fixture, map[string]any{"sql": "SELECT 1", "max_rows": 1000, "timeout_seconds": 300})
}
