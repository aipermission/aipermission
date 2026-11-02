package conformance_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sqlserverconnector "github.com/aipermission/aipermission/backend/internal/connectors/sqlserver"
)

func assertSQLServerExecutingQueryCancellation(t *testing.T, runtime connectors.RuntimeContext, certificate []byte) {
	t.Helper()
	writer := sqlServerFixtureDB(t, runtime, certificate, "fixture_policy_writer", "Fixture-only-Policy1!")
	defer writer.Close()
	transaction, err := writer.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	var blocker int
	if err := transaction.QueryRowContext(t.Context(), "SELECT @@SPID").Scan(&blocker); err != nil {
		t.Fatal(err)
	}
	if _, err := transaction.ExecContext(t.Context(), "UPDATE dbo.policy_canary SET value=value"); err != nil {
		t.Fatal(err)
	}
	// Administrative diagnostics are confined to the disposable fixture. The
	// connector action itself still uses the least-privilege reader login.
	observer := sqlServerFixtureDB(t, runtime, certificate, "sa", "Fixture-only-Password1!")
	defer observer.Close()
	connector := sqlserverconnector.New()
	prepared, err := connector.PrepareAction(t.Context(), connectors.ActionRequest{Target: runtime.Target, Profile: runtime.Profile,
		ActionName: "query_readonly", Input: map[string]any{"sql": "SELECT value FROM dbo.policy_canary", "max_rows": 1}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		result, err := connector.ExecuteAction(ctx, runtime, prepared)
		if result.Status == connectors.ResultCompleted && err == nil {
			err = errors.New("blocked fixture query unexpectedly completed")
		}
		done <- err
	}()
	waitForSQLServerBlockedSelect(t, observer, blocker, done)
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("query cancellation=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatalf("executing query ignored cancellation after %s", time.Since(started))
	}
}

func waitForSQLServerBlockedSelect(t *testing.T, observer *sql.DB, blocker int, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var count int
		if err := observer.QueryRowContext(ctx, `SELECT COUNT(*) FROM sys.dm_exec_requests WHERE blocking_session_id=@blocker AND command='SELECT'`, sql.Named("blocker", blocker)).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count > 0 {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("action ended before reaching the fixture row lock: %v", err)
		case <-ctx.Done():
			t.Fatal("action never reached the executing blocked SELECT")
		case <-ticker.C:
		}
	}
}
