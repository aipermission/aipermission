package conformance_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestSQLServerExecutionRealService(t *testing.T) {
	requireConformance(t)
	t.Run("input_contract", func(t *testing.T) {
		assertSQLExecutionInputContract(t, newSQLServerExecutionFixture(t).sqlExecutionFixture)
	})
	t.Run("ddl_dml_readback", func(t *testing.T) {
		assertSQLExecutionLifecycle(t, newSQLServerExecutionFixture(t).sqlExecutionFixture, "\nGO\n", 2)
	})
	t.Run("multiple_result_sets", func(t *testing.T) {
		assertSQLExecutionMultipleResults(t, newSQLServerExecutionFixture(t).sqlExecutionFixture)
	})
	t.Run("row_cap_drains_later_mutations", func(t *testing.T) {
		fixture := newSQLServerExecutionFixture(t)
		assertSQLExecutionRowCap(t, fixture.sqlExecutionFixture, "\nGO\n")
		if value := fixture.value(t); value != 2 {
			t.Fatalf("capped output prevented later mutation: %d", value)
		}
	})
	t.Run("procedure_exec_drop", testSQLServerSQLExecutionProcedure)
	t.Run("go_quoted_strings", testSQLServerSQLExecutionGO)
	t.Run("readonly_profile", testSQLServerSQLExecutionReadonly)
	t.Run("open_transaction_cleanup", testSQLServerSQLExecutionTransaction)
	t.Run("committed_prefix_error_no_replay", testSQLServerSQLExecutionError)
	t.Run("committed_prefix_cancellation_no_replay", testSQLServerSQLExecutionCancellation)
}

func testSQLServerSQLExecutionProcedure(t *testing.T) {
	fixture := newSQLServerExecutionFixture(t)
	script := fmt.Sprintf(`CREATE PROCEDURE %s AS BEGIN
UPDATE %s SET value=value+1 WHERE id=1;
SELECT value FROM %s WHERE id=1;
SELECT 22 AS second_value;
PRINT N'owned-procedure'; END;
GO
EXEC %s;
GO
DROP PROCEDURE %s;
GO
SELECT value FROM %s WHERE id=1;`, fixture.procedure, fixture.table, fixture.table, fixture.procedure, fixture.procedure, fixture.table)
	output := runSQLExecution(t, fixture.sqlExecutionFixture, script, 100)
	if output.CompletedBatches != 4 || len(output.ResultSets) != 3 || fixture.value(t) != 1 {
		t.Fatalf("procedure/GO/result-set contract: %#v", output)
	}
	assertSQLExecutionValue(t, output, 0, "value", "1")
	assertSQLExecutionValue(t, output, 1, "second_value", "22")
	assertSQLExecutionValue(t, output, 2, "value", "1")
	if !strings.Contains(strings.Join(output.Notices, "\n"), "owned-procedure") {
		t.Fatalf("PRINT notice lost: %#v", output)
	}
	var exists int
	if err := fixture.observer.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sys.procedures WHERE object_id=OBJECT_ID(@name)",
		sql.Named("name", fixture.procedure)).Scan(&exists); err != nil || exists != 0 {
		t.Fatalf("procedure was not dropped: count=%d err=%v", exists, err)
	}
}

func testSQLServerSQLExecutionGO(t *testing.T) {
	fixture := newSQLServerExecutionFixture(t)
	marker := "before'quote\nGO\nafter; UPDATE " + fixture.table + " SET value=99"
	script := "SELECT N'" + strings.ReplaceAll(marker, "'", "''") + "' AS marker;\n" +
		"-- GO is not a separator inside a comment\n/*\nGO\n*/\nGO\n" +
		"UPDATE " + fixture.table + " SET value=value+1 WHERE id=1;\nGO\n" +
		"SELECT value FROM " + fixture.table + " WHERE id=1;"
	output := runSQLExecution(t, fixture.sqlExecutionFixture, script, 100)
	if output.CompletedBatches != 3 {
		t.Fatalf("quoted/comment GO was split as a real batch: %#v", output)
	}
	assertSQLExecutionValue(t, output, 0, "marker", marker)
	assertSQLExecutionValue(t, output, 1, "value", "1")
	if value := fixture.value(t); value != 1 {
		t.Fatalf("quoted SQL was executed or GO replayed: %d", value)
	}
	_, err := fixture.connector.PrepareAction(t.Context(), connectorsSQLRequest(fixture.sqlExecutionFixture,
		map[string]any{"sql": "UPDATE " + fixture.table + " SET value=value+1;\nGO 2\n"}))
	if err == nil {
		t.Fatal("GO repeat count was accepted")
	}
}

func testSQLServerSQLExecutionReadonly(t *testing.T) {
	fixture := newSQLServerExecutionFixture(t)
	reader := fixture.readonly()
	read := executeAction(t, reader.connector, reader.runtime, "query_readonly", map[string]any{"sql": "SELECT value FROM " + fixture.table})
	assertResultContains(t, read, "value")
	assertSQLExecutionDenied(t, reader, "UPDATE "+fixture.table+" SET value=99 WHERE id=1")
	assertSQLExecutionDenied(t, reader, "ALTER TABLE "+fixture.table+" ADD forbidden int")
	runSQLExecution(t, fixture.sqlExecutionFixture, "CREATE PROCEDURE "+fixture.procedure+" AS UPDATE "+fixture.table+" SET value=99 WHERE id=1;", 100)
	assertSQLExecutionDenied(t, reader, "EXEC "+fixture.procedure)
	if value := fixture.value(t); value != 0 {
		t.Fatalf("readonly execution mutated fixture: %d", value)
	}
}
