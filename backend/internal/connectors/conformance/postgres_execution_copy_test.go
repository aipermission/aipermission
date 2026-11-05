package conformance_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func testPostgresSQLExecutionCopy(t *testing.T) {
	for _, direction := range []string{"FROM STDIN", "TO STDOUT"} {
		t.Run(direction, func(t *testing.T) {
			fixture := newPostgresExecutionFixture(t)
			script := fmt.Sprintf("BEGIN; UPDATE %s SET value=7 WHERE id=1; COMMIT; COPY %s (value) %s;", fixture.table, fixture.table, direction)
			prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{"sql": script, "timeout_seconds": 30})
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			started := time.Now()
			result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
			assertSQLExecutionUnknown(t, result, err)
			if time.Since(started) > 3*time.Second || !strings.Contains(result.Error+fmt.Sprint(err), "COPY client transfer is not supported") {
				t.Fatalf("unsupported COPY hung or appeared successful: %#v %v", result, err)
			}
			if value := fixture.value(t); value != 7 {
				t.Fatalf("explicitly committed prefix was lost/replayed: %d", value)
			}
			assertPostgresSQLCommitAcknowledged(t, decodeSQLExecution(t, result))
		})
	}
}

func testPostgresSQLImplicitRollback(t *testing.T) {
	fixture := newPostgresExecutionFixture(t)
	prepared := prepareSQLExecution(t, fixture.sqlExecutionFixture, map[string]any{
		"sql": "UPDATE " + fixture.table + " SET value=7 WHERE id=1; DO $$ BEGIN RAISE EXCEPTION 'implicit-rollback'; END $$;",
	})
	result, err := fixture.connector.ExecuteAction(t.Context(), fixture.runtime, prepared)
	assertSQLExecutionUnknown(t, result, err)
	if !strings.Contains(result.Error+fmt.Sprint(err), "implicit-rollback") || fixture.value(t) != 0 {
		t.Fatalf("simple-query implicit transaction failed to roll back: %#v %v", result, err)
	}
}
