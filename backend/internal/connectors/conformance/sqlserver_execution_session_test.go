package conformance_test

import (
	"fmt"
	"testing"
)

func testSQLServerSQLExecutionSession(t *testing.T) {
	f := newSQLServerExecutionFixture(t)
	script := "CREATE TABLE #owned_session (n int); INSERT INTO #owned_session VALUES (4); SELECT @@SPID AS session_id;\r\nGO\r\n" +
		"BEGIN TRANSACTION; UPDATE " + f.table + " SET value=19; SELECT @@SPID AS session_id;\rGO\r" +
		"UPDATE " + f.table + " SET value=value+1; COMMIT; SELECT n FROM #owned_session; SELECT @@SPID AS session_id;"
	out := runSQLExecution(t, f.sqlExecutionFixture, script, 100)
	if f.value(t) != 20 || len(out.ResultSets) != 4 || out.CompletedBatches != 3 {
		t.Fatalf("physical session or transaction lost: %#v", out)
	}
	session := fmt.Sprint(out.ResultSets[0].Rows[0]["session_id"])
	for _, set := range []int{1, 3} {
		assertSQLExecutionValue(t, out, set, "session_id", session)
	}
	assertSQLExecutionValue(t, out, 2, "n", "4")
	prepared := prepareSQLExecution(t, f.sqlExecutionFixture, map[string]any{"sql": "SELECT * FROM #owned_session;"})
	result, err := f.connector.ExecuteAction(t.Context(), f.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if f.value(t) != 20 {
		t.Fatal("fresh action changed committed work")
	}
}
