package conformance_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testSQLServerSQLExecutionTransaction(t *testing.T) {
	fixture := newSQLServerExecutionFixture(t)
	prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{
		"sql": "BEGIN TRANSACTION; UPDATE " + fixture.table + " SET value=1 WHERE id=1; SELECT value FROM " + fixture.table + " WHERE id=1;",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if err == nil {
		assertSQLExecutionValue(t, decodeSQLExecution(t, result), 0, "value", "1")
	}
	// Locking READ COMMITTED must not block behind an abandoned transaction.
	if value := fixture.value(t); value != 0 {
		t.Fatalf("unfinished transaction was committed instead of rolled back: %d", value)
	}
	output := runSQLExecution(t, fixture.sqlExecutionFixture, "SELECT @@TRANCOUNT AS depth, value FROM "+fixture.table+" WHERE id=1", 100)
	assertSQLExecutionValue(t, output, 0, "depth", "0")
	assertSQLExecutionValue(t, output, 0, "value", "0")
}

func testSQLServerSQLExecutionError(t *testing.T) {
	fixture := newSQLServerExecutionFixture(t)
	script := fmt.Sprintf(`BEGIN TRANSACTION; UPDATE %s SET value=value+1 WHERE id=1; COMMIT;
GO
THROW 51000, 'owned-execution-failure', 1;
GO
UPDATE %s SET value=value+100 WHERE id=1;`, fixture.table, fixture.table)
	prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{"sql": script})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if !strings.Contains(result.Error+fmt.Sprint(err), "owned-execution-failure") {
		t.Fatalf("expected intentional post-commit server failure: %#v %v", result, err)
	}
	if err == nil && decodeSQLExecution(t, result).CompletedBatches != 1 {
		t.Fatalf("acknowledged GO prefix was not retained: %#v", result)
	}
	if value := fixture.value(t); value != 1 {
		t.Fatalf("uncertain script was replayed or its later batch ran: %d", value)
	}
	for _, prefix := range []string{
		"RAISERROR(N'owned-batch-error',16,1) WITH NOWAIT; WAITFOR DELAY '00:00:01';",
		"INSERT INTO " + fixture.table + " VALUES (1,99);",
		"SELECT SERVERPROPERTY('ProductMajorVersion');",
	} {
		if _, err := fixture.observer.ExecContext(ctx, "UPDATE "+fixture.table+" SET value=0 WHERE id=1"); err != nil {
			t.Fatal(err)
		}
		prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{
			"sql": prefix + " UPDATE " + fixture.table + " SET value=7 WHERE id=1;\nGO\nUPDATE " + fixture.table + " SET value=100 WHERE id=1;",
		})
		result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
		assertSQLExecutionUnknown(t, result, err)
		if value := fixture.value(t); value != 7 {
			t.Fatalf("current batch was interrupted or a later batch ran: prefix=%q value=%d", prefix, value)
		}
	}
}

func testSQLServerSQLExecutionCancellation(t *testing.T) {
	fixture := newSQLServerExecutionFixture(t)
	// Only the disposable fixture's diagnostic connection is administrative;
	// execute_sql and all object setup/cleanup use fixture_executor.
	observer := sqlServerFixtureDB(t, fixture.runtime, fixture.certificate, "sa", "Fixture-only-Password1!")
	t.Cleanup(func() { _ = observer.Close() })
	script := fmt.Sprintf(`BEGIN TRANSACTION; UPDATE %s SET value=value+1 WHERE id=1; COMMIT;
GO
WAITFOR DELAY '00:01:00'; UPDATE %s SET value=value+100 WHERE id=1;`, fixture.table, fixture.table)
	cancel, done := startSQLExecution(t, fixture.sqlExecutionFixture, script)
	waitSQLExecutionWitness(t, done, func(ctx context.Context) (bool, error) {
		var waiting int
		err := observer.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys.dm_exec_requests AS r
CROSS APPLY sys.dm_exec_sql_text(r.sql_handle) AS s
WHERE r.database_id=DB_ID() AND r.command='WAITFOR' AND CHARINDEX(@name,s.text)>0`,
			sql.Named("name", fixture.table)).Scan(&waiting)
		return waiting == 1, err
	})
	if value := fixture.value(t); value != 1 {
		t.Fatalf("prefix was not independently committed before cancellation: %d", value)
	}
	completion := finishCanceledSQLExecution(t, cancel, done)
	if completion.err == nil && decodeSQLExecution(t, completion.result).CompletedBatches != 1 {
		t.Fatalf("acknowledged GO prefix was lost after cancellation: %#v", completion.result)
	}
	if value := fixture.value(t); value != 1 {
		t.Fatalf("canceled uncertain execution was replayed or ran its tail: %d", value)
	}
	assertSQLExecutionValue(t, runSQLExecution(t, fixture.sqlExecutionFixture, "SELECT value FROM "+fixture.table+" WHERE id=1", 100), 0, "value", "1")
}
