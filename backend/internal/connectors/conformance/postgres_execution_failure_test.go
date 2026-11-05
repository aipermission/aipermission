package conformance_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testPostgresSQLExecutionTransaction(t *testing.T) {
	fixture := newPostgresExecutionFixture(t)
	prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{
		"sql": "BEGIN; UPDATE " + fixture.table + " SET value=1 WHERE id=1; SELECT value FROM " + fixture.table + " WHERE id=1;",
	})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if err == nil {
		assertSQLExecutionValue(t, decodeSQLExecution(t, result), 0, "value", "1")
	}
	// An MVCC read alone would not prove rollback: this write must acquire the
	// original row lock, with the observer's one-second lock timeout.
	if _, err := fixture.observer.Exec(ctx, "UPDATE "+fixture.table+" SET value=value WHERE id=1"); err != nil {
		t.Fatalf("unfinished transaction retained its row lock: %v", err)
	}
	if value := fixture.value(t); value != 0 {
		t.Fatalf("unfinished transaction was committed instead of rolled back: %d", value)
	}
	assertSQLExecutionValue(t, runSQLExecution(t, fixture.sqlExecutionFixture, "SELECT value FROM "+fixture.table+" WHERE id=1", 100), 0, "value", "0")
}

func testPostgresSQLExecutionError(t *testing.T) {
	fixture := newPostgresExecutionFixture(t)
	script := fmt.Sprintf(`BEGIN; UPDATE %s SET value=value+1 WHERE id=1; COMMIT;
DO $fixture$ BEGIN RAISE EXCEPTION 'owned-execution-failure'; END; $fixture$;
UPDATE %s SET value=value+100 WHERE id=1;`, fixture.table, fixture.table)
	prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{"sql": script})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if !strings.Contains(result.Error+fmt.Sprint(err), "owned-execution-failure") {
		t.Fatalf("expected intentional post-commit server failure: %#v %v", result, err)
	}
	if err == nil {
		assertPostgresSQLCommitAcknowledged(t, decodeSQLExecution(t, result))
	}
	if value := fixture.value(t); value != 1 {
		t.Fatalf("uncertain script was replayed or its later mutation ran: %d", value)
	}
}

func testPostgresSQLExecutionCancellation(t *testing.T) {
	fixture := newPostgresExecutionFixture(t)
	// Force a protocol flush before sleeping so the committed prefix really is
	// observable by the client before cancellation, not only by the observer.
	script := fmt.Sprintf(`BEGIN; UPDATE %s SET value=value+1 WHERE id=1; COMMIT;
SELECT repeat('x',16384) AS flushed_prefix;
SELECT pg_sleep(60); UPDATE %s SET value=value+100 WHERE id=1;`, fixture.table, fixture.table)
	cancel, done := startSQLExecution(t, fixture.sqlExecutionFixture, script)
	waitSQLExecutionWitness(t, done, func(ctx context.Context) (bool, error) {
		var waiting bool
		err := fixture.observer.QueryRow(ctx, `SELECT EXISTS (
SELECT 1 FROM pg_catalog.pg_stat_activity WHERE datname=current_database()
AND pid<>pg_backend_pid() AND wait_event='PgSleep' AND position($1 IN query)>0)`, fixture.table).Scan(&waiting)
		return waiting, err
	})
	if value := fixture.value(t); value != 1 {
		t.Fatalf("prefix was not independently committed before cancellation: %d", value)
	}
	completion := finishCanceledSQLExecution(t, cancel, done)
	if completion.err == nil {
		assertPostgresSQLCommitAcknowledged(t, decodeSQLExecution(t, completion.result))
	}
	if value := fixture.value(t); value != 1 {
		t.Fatalf("canceled uncertain execution was replayed or ran its tail: %d", value)
	}
	assertSQLExecutionValue(t, runSQLExecution(t, fixture.sqlExecutionFixture, "SELECT value FROM "+fixture.table+" WHERE id=1", 100), 0, "value", "1")
}

func assertPostgresSQLCommitAcknowledged(t *testing.T, output sqlExecutionOutput) {
	t.Helper()
	for _, command := range output.Commands {
		if command.Tag == "COMMIT" {
			return
		}
	}
	t.Fatalf("committed prefix acknowledgement lost: %#v", output)
}
