package httptransport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/sqlexecute"
)

type deadlineSQLSession struct {
	execute func(context.Context) error
	close   func(context.Context)
}

func (s deadlineSQLSession) Execute(ctx context.Context, _ string, _ *sqlexecute.Collector) error {
	return s.execute(ctx)
}
func (deadlineSQLSession) Finish(context.Context) error { return nil }
func (s deadlineSQLSession) Close(ctx context.Context) error {
	if s.close != nil {
		s.close(ctx)
	}
	return nil
}

func TestActionHTTPBudgetPreservesAcceptedSQLTimeout(t *testing.T) {
	for _, kind := range []string{"postgres", "sqlserver"} {
		for _, path := range []string{"/api/connector-actions/local-run", "/api/mcp/connector-actions/call", "/api/connector-action-approvals/1/run"} {
			for _, seconds := range []int{30, 90, 120, 300} {
				t.Run(fmt.Sprintf("%s/%s/%d", kind, path, seconds), func(t *testing.T) {
					assertSQLDeadlineAssembly(t, kind, path, seconds)
				})
			}
		}
	}
}

func TestBoundaryUsesInjectedActionBudgetAndBoundsMissingConfiguration(t *testing.T) {
	for _, configured := range []time.Duration{0, connectors.ActionRequestTimeout} {
		want := configured
		if want == 0 {
			want = OrdinaryRequestTimeout
		}
		for _, path := range []string{"/api/connector-actions/local-run", "/api/mcp/connector-actions/call", "/api/connector-action-approvals/1/run"} {
			called := false
			boundary := HTTPBoundary{
				ActionRequestTimeout: configured, Lifecycle: openLifecycle{},
				IsUnlocked: func() bool { return true }, IsLocalRemoteAddr: func(string) bool { return true },
				IsLocalhostHeader: func(string) bool { return true }, HasSession: func(*http.Request) bool { return true },
				Routes: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					deadline, ok := r.Context().Deadline()
					if !ok || time.Until(deadline) < want-time.Second || time.Until(deadline) > want {
						t.Errorf("boundary lost configured %s budget", want)
					}
					w.WriteHeader(http.StatusNoContent)
				}),
			}
			response := httptest.NewRecorder()
			boundary.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
			if !called || response.Code != http.StatusNoContent {
				t.Fatalf("boundary did not dispatch: %d", response.Code)
			}
		}
	}
}

func assertSQLDeadlineAssembly(t *testing.T, kind, path string, seconds int) {
	t.Helper()
	writer := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler := WithRequestDeadline(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action, err := sqlexecute.Prepare(connectors.ActionRequest{ActionName: sqlexecute.Action,
			Input: map[string]any{"sql": "SELECT 1", "max_rows": 1, "timeout_seconds": seconds}}, kind, kind)
		if err != nil {
			t.Fatal(err)
		}
		result, err := sqlexecute.Run(r.Context(), action, []string{"SELECT 1"}, func(context.Context) (sqlexecute.Session, error) {
			return deadlineSQLSession{execute: func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				left := time.Until(deadline)
				selected := time.Duration(seconds) * time.Second
				if !ok || left < selected-2*time.Second || left > selected {
					t.Errorf("selected %s reduced to %s", selected, left)
				}
				if time.Until(writer.writeDeadline) < selected+25*time.Second {
					t.Error("HTTP response deadline leaves insufficient finalization budget")
				}
				return nil
			}}, nil
		})
		if err != nil || result.Status != connectors.ResultCompleted {
			t.Fatalf("assembled run: %#v %v", result, err)
		}
		w.WriteHeader(http.StatusNoContent)
	}), OrdinaryRequestTimeout, connectors.ActionRequestTimeout)
	handler.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, path, nil))
	if writer.Code != http.StatusNoContent || !writer.writeDeadline.IsZero() {
		t.Fatalf("response/deadline cleanup: %d %s", writer.Code, writer.writeDeadline)
	}
}

func TestSQLCallerCancellationKeepsBoundedCleanupAndUnknownOutcome(t *testing.T) {
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	closed := false
	action, err := sqlexecute.Prepare(connectors.ActionRequest{ActionName: sqlexecute.Action,
		Input: map[string]any{"sql": "SELECT 1", "max_rows": 1, "timeout_seconds": 300}}, "postgres", "Postgres")
	if err != nil {
		t.Fatal(err)
	}
	handler := WithRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		result, err := sqlexecute.Run(r.Context(), action, []string{"SELECT 1"}, func(context.Context) (sqlexecute.Session, error) {
			return deadlineSQLSession{execute: func(ctx context.Context) error {
				cancel()
				<-ctx.Done()
				return ctx.Err()
			}, close: func(ctx context.Context) {
				deadline, ok := ctx.Deadline()
				if !ok || ctx.Err() != nil || time.Until(deadline) > 3*time.Second {
					t.Error("cleanup inherited cancellation or became unbounded")
				}
				closed = true
			}}, nil
		})
		if err != nil || result.Status != connectors.ResultOutcomeUnknown || !errors.Is(parent.Err(), context.Canceled) {
			t.Fatalf("canceled post-dispatch result: %#v %v", result, err)
		}
	}), OrdinaryRequestTimeout, connectors.ActionRequestTimeout)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/connector-actions/local-run", nil).WithContext(parent))
	if !closed {
		t.Fatal("canceled SQL session was not closed")
	}
}

func TestSQLShorterCallerDeadlineStillWins(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	seen := false
	handler := WithRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		action, err := sqlexecute.Prepare(connectors.ActionRequest{ActionName: sqlexecute.Action,
			Input: map[string]any{"sql": "SELECT 1", "max_rows": 1, "timeout_seconds": 300}}, "sqlserver", "SQL Server")
		if err != nil {
			t.Fatal(err)
		}
		_, err = sqlexecute.Run(r.Context(), action, []string{"SELECT 1"}, func(context.Context) (sqlexecute.Session, error) {
			return deadlineSQLSession{execute: func(ctx context.Context) error {
				deadline, ok := ctx.Deadline()
				if !ok || time.Until(deadline) > time.Second {
					t.Error("longer action budget replaced the caller deadline")
				}
				seen = true
				return nil
			}}, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}), OrdinaryRequestTimeout, connectors.ActionRequestTimeout)
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/mcp/connector-actions/call", nil).WithContext(ctx))
	if !seen {
		t.Fatal("SQL session not dispatched")
	}
}
