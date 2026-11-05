package sqlexecute

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
)

type fixtureSession struct {
	steps     []string
	execute   func(context.Context, string, *Collector) error
	finishErr error
	closed    bool
}

func (f *fixtureSession) Execute(ctx context.Context, sql string, out *Collector) error {
	f.steps = append(f.steps, sql)
	if f.execute != nil {
		return f.execute(ctx, sql, out)
	}
	return nil
}
func (f *fixtureSession) Finish(context.Context) error { return f.finishErr }
func (f *fixtureSession) Close(ctx context.Context) error {
	f.closed = true
	return ctx.Err()
}

func preparedFixture(t *testing.T) connectors.PreparedAction {
	t.Helper()
	prepared, err := Prepare(connectors.ActionRequest{ActionName: Action, Input: map[string]any{"sql": "SELECT 1", "max_rows": 1}}, "fixture", "fixture SQL")
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func TestOutputLimitsNeverSkipLaterMutations(t *testing.T) {
	session := &fixtureSession{execute: func(_ context.Context, _ string, out *Collector) error {
		if err := out.BeginResult([]string{"value"}); err != nil {
			return err
		}
		for i := 0; i < 3; i++ {
			if err := out.AddRow([]any{i}); err != nil {
				return err
			}
		}
		out.EndResult()
		out.Command("UPDATE", 3)
		return nil
	}}
	result, err := Run(t.Context(), preparedFixture(t), []string{"first", "second", "third"}, func(context.Context) (Session, error) { return session, nil })
	if err != nil || result.Status != connectors.ResultCompleted || len(session.steps) != 3 || !session.closed {
		t.Fatalf("truncated output stopped a script: %#v %v %+v", result, err, session)
	}
	output := result.Output.(map[string]any)
	if output["row_count"] != 1 || output["truncated"] != true || output["completed_batches"] != 3 || len(output["commands"].([]Command)) != 3 {
		t.Fatalf("unexpected bounded result: %#v", output)
	}
}

func TestNoAutomaticReplayAfterAnyDispatchedFailure(t *testing.T) {
	for _, cause := range []error{errors.New("server rejected later statement"), context.Canceled, context.DeadlineExceeded} {
		session := &fixtureSession{execute: func(_ context.Context, _ string, out *Collector) error {
			out.Notice("earlier work may already have committed")
			return cause
		}}
		result, err := Run(t.Context(), preparedFixture(t), []string{"first", "must-not-run"}, func(context.Context) (Session, error) { return session, nil })
		if err != nil || result.Status != connectors.ResultOutcomeUnknown || len(session.steps) != 1 || !session.closed {
			t.Fatalf("uncertain dispatch was retried or mislabeled: %#v %v", result, err)
		}
		if output := result.Output.(map[string]any); output["retry_safe"] != false || output["dispatch_stage"] != "sql_execution" {
			t.Fatalf("lost canonical unknown-outcome contract: %#v", output)
		}
	}
}

func TestPreDispatchFailureAndTransactionFinalization(t *testing.T) {
	openErr := errors.New("credential unavailable")
	if _, err := Run(t.Context(), preparedFixture(t), []string{"SELECT 1"}, func(context.Context) (Session, error) { return nil, openErr }); !errors.Is(err, openErr) {
		t.Fatal("connection failure was mislabeled as dispatched")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opened := false
	_, err := Run(ctx, preparedFixture(t), []string{"SELECT 1"}, func(context.Context) (Session, error) { opened = true; return nil, openErr })
	if !errors.Is(err, context.Canceled) || opened {
		t.Fatal("canceled action opened a session")
	}
	session := &fixtureSession{finishErr: errors.New("script left a transaction open")}
	result, err := Run(t.Context(), preparedFixture(t), []string{"BEGIN"}, func(context.Context) (Session, error) { return session, nil })
	if err != nil || result.Status != connectors.ResultOutcomeUnknown || !session.closed {
		t.Fatal("open transaction reported successful completion")
	}
}

func TestInvalidPlansNeverAcquireCredentials(t *testing.T) {
	for _, batches := range [][]string{nil, {" "}, {strings.Repeat("x", MaxSQLBytes+1)}, make([]string, MaxBatches+1)} {
		opened := false
		_, err := Run(t.Context(), preparedFixture(t), batches, func(context.Context) (Session, error) { opened = true; return nil, errors.New("unexpected open") })
		if err == nil || opened {
			t.Fatal("invalid plan reached execution")
		}
	}
}

func TestWideResultDoesNotSkipLaterBatch(t *testing.T) {
	session := &fixtureSession{execute: func(_ context.Context, _ string, out *Collector) error {
		if err := out.BeginResult(make([]string, 257)); err != nil {
			return err
		}
		if out.RetainingRows() {
			t.Fatal("wide metadata retained")
		}
		out.EndResult()
		return nil
	}}
	result, err := Run(t.Context(), preparedFixture(t), []string{"wide", "later mutation"}, func(context.Context) (Session, error) { return session, nil })
	if err != nil || result.Status != connectors.ResultCompleted || len(session.steps) != 2 || result.Output.(map[string]any)["truncated"] != true {
		t.Fatalf("wide result interrupted later mutation: %#v %v", result, err)
	}
}

func TestUnknownDiagnosticIsBoundedWithEscapedServerError(t *testing.T) {
	session := &fixtureSession{execute: func(_ context.Context, _ string, out *Collector) error {
		return errors.New(strings.Repeat("\x00", MaxOutputBytes))
	}}
	result, err := Run(t.Context(), preparedFixture(t), []string{"mutation"}, func(context.Context) (Session, error) { return session, nil })
	encoded, marshalErr := json.Marshal(result)
	if err != nil || marshalErr != nil || result.Status != connectors.ResultOutcomeUnknown || len(result.Error) > 2048 || len(encoded) > MaxOutputBytes {
		t.Fatalf("oversized SQL error escaped response budget: %d bytes, err=%v marshal=%v", len(encoded), err, marshalErr)
	}
}

func TestCancellationBetweenBatchesPreservesAcknowledgedPrefix(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	session := &fixtureSession{execute: func(_ context.Context, _ string, out *Collector) error {
		out.Command("COMMIT", 0)
		cancel()
		return nil
	}}
	result, err := Run(ctx, preparedFixture(t), []string{"committed prefix", "never send"}, func(context.Context) (Session, error) { return session, nil })
	if err != nil || result.Status != connectors.ResultOutcomeUnknown || len(session.steps) != 1 || !session.closed {
		t.Fatalf("cancellation replayed or discarded acknowledged prefix: %#v %v", result, err)
	}
	if output := result.Output.(map[string]any); output["completed_batches"] != 1 || len(output["commands"].([]Command)) != 1 {
		t.Fatalf("partial completion evidence lost: %#v", output)
	}
}
