package conformance_test

import (
	"context"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type sqlExecutionCompletion struct {
	result connectors.ActionResult
	err    error
}

func startSQLExecution(t *testing.T, fixture sqlExecutionFixture, script string) (context.CancelFunc, <-chan sqlExecutionCompletion) {
	t.Helper()
	prepared := prepareSQLExecution(t, fixture, map[string]any{"sql": script, "timeout_seconds": 30})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	done := make(chan sqlExecutionCompletion, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		result, err := fixture.connector.ExecuteAction(ctx, fixture.runtime, prepared)
		done <- sqlExecutionCompletion{result: result, err: err}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Error("fixture SQL execution did not finish after cancellation")
		}
	})
	return cancel, done
}

func waitSQLExecutionWitness(t *testing.T, done <-chan sqlExecutionCompletion, witness func(context.Context) (bool, error)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		seen, err := witness(ctx)
		if err != nil {
			t.Fatalf("observe owned committed-prefix fixture: %v", err)
		}
		if seen {
			return
		}
		select {
		case completion := <-done:
			t.Fatalf("execution ended before acknowledged prefix witness: %#v %v", completion.result, completion.err)
		case <-ctx.Done():
			t.Fatal("execution never reached the wait after its committed prefix")
		case <-ticker.C:
		}
	}
}

func finishCanceledSQLExecution(t *testing.T, cancel context.CancelFunc, done <-chan sqlExecutionCompletion) sqlExecutionCompletion {
	t.Helper()
	cancel()
	select {
	case completion := <-done:
		assertSQLExecutionUnknown(t, completion.result, completion.err)
		return completion
	case <-time.After(5 * time.Second):
		t.Fatal("dispatched SQL execution ignored cancellation")
		return sqlExecutionCompletion{}
	}
}
