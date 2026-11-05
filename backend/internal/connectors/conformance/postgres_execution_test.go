package conformance_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestPostgresExecutionRealService(t *testing.T) {
	requireConformance(t)
	t.Run("input_contract", func(t *testing.T) {
		assertSQLExecutionInputContract(t, newPostgresExecutionFixture(t).sqlExecutionFixture)
	})
	t.Run("ddl_dml_readback", func(t *testing.T) {
		assertSQLExecutionLifecycle(t, newPostgresExecutionFixture(t).sqlExecutionFixture, "", 1)
	})
	t.Run("multiple_result_sets", func(t *testing.T) {
		assertSQLExecutionMultipleResults(t, newPostgresExecutionFixture(t).sqlExecutionFixture)
	})
	t.Run("row_cap_drains_later_mutations", func(t *testing.T) {
		fixture := newPostgresExecutionFixture(t)
		assertSQLExecutionRowCap(t, fixture.sqlExecutionFixture, "\n")
		if value := fixture.value(t); value != 2 {
			t.Fatalf("capped output prevented later mutation: %d", value)
		}
	})
	t.Run("procedure_and_do", testPostgresSQLExecutionProcedure)
	t.Run("readonly_profile", testPostgresSQLExecutionReadonly)
	t.Run("open_transaction_cleanup", testPostgresSQLExecutionTransaction)
	t.Run("committed_prefix_error_no_replay", testPostgresSQLExecutionError)
	t.Run("committed_prefix_cancellation_no_replay", testPostgresSQLExecutionCancellation)
}

func testPostgresSQLExecutionProcedure(t *testing.T) {
	fixture := newPostgresExecutionFixture(t)
	script := fmt.Sprintf(`CREATE PROCEDURE %s() LANGUAGE plpgsql AS $fixture$
BEGIN UPDATE %s SET value=value+1 WHERE id=1; RAISE NOTICE 'owned-procedure'; END; $fixture$;
CALL %s();
DO $fixture$ BEGIN UPDATE %s SET value=value+1 WHERE id=1; END; $fixture$;
SELECT value FROM %s WHERE id=1;
DROP PROCEDURE %s();`, fixture.procedure, fixture.table, fixture.procedure, fixture.table, fixture.table, fixture.procedure)
	output := runSQLExecution(t, fixture.sqlExecutionFixture, script, 100)
	assertSQLExecutionValue(t, output, 0, "value", "2")
	if !strings.Contains(strings.Join(output.Notices, "\n"), "owned-procedure") || fixture.value(t) != 2 {
		t.Fatalf("CALL/DO did not execute or lost NOTICE: %#v", output)
	}
	var exists bool
	if err := fixture.observer.QueryRow(t.Context(), "SELECT to_regprocedure($1) IS NOT NULL", fixture.procedure+"()").Scan(&exists); err != nil || exists {
		t.Fatalf("procedure was not dropped: exists=%t err=%v", exists, err)
	}
}

func testPostgresSQLExecutionReadonly(t *testing.T) {
	fixture := newPostgresExecutionFixture(t)
	reader := fixture.readonly(t)
	read := executeAction(t, reader.connector, reader.runtime, "query_readonly", map[string]any{"sql": "SELECT value FROM " + fixture.table})
	assertResultContains(t, read, "value")
	assertSQLExecutionDenied(t, reader, "UPDATE "+fixture.table+" SET value=99 WHERE id=1")
	assertSQLExecutionDenied(t, reader, "ALTER TABLE "+fixture.table+" ADD forbidden int")
	runSQLExecution(t, fixture.sqlExecutionFixture, "CREATE PROCEDURE "+fixture.procedure+
		"() LANGUAGE plpgsql AS $fixture$ BEGIN UPDATE "+fixture.table+" SET value=99 WHERE id=1; END; $fixture$;", 100)
	assertSQLExecutionDenied(t, reader, "CALL "+fixture.procedure+"()")
	if value := fixture.value(t); value != 0 {
		t.Fatalf("readonly execution mutated fixture: %d", value)
	}
}
