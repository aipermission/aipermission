package actions

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeWorkflowAuditFailurePreservesAdmissionAndDispatchedFinality(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "admission", true: "terminal"}[terminal], func(t *testing.T) {
			fixture := newNativeWorkflowFixture(t, connectortargets.ActionPermissionAlwaysRun, connectors.RiskWrite)
			action := "connector_action.request.created"
			if terminal {
				action = "connector_action.request.completed"
			}
			if _, err := fixture.database.ExecContext(t.Context(), `CREATE TRIGGER fail_native_workflow_audit BEFORE INSERT ON audit_outbox WHEN NEW.action = '`+action+`' BEGIN SELECT RAISE(ABORT, 'injected native workflow audit failure'); END`); err != nil {
				t.Fatal(err)
			}
			_, err := fixture.runtime.Call(t.Context(), fixture.call)
			var persistence *TerminalPersistenceError
			if errors.As(err, &persistence) != terminal {
				t.Fatalf("failure lost pre/post-dispatch distinction: %v", err)
			}
			cause := err
			if persistence != nil {
				cause = persistence.Err
			}
			if cause == nil || !strings.Contains(cause.Error(), "injected native workflow audit failure") {
				t.Fatalf("required audit failure not reached: %v", cause)
			}
			wantCalls := int64(0)
			if terminal {
				wantCalls = 1
			}
			if fixture.connector.dispatches.Load() != wantCalls {
				t.Fatal("audit failure changed dispatch count")
			}
			if !terminal {
				for _, table := range []string{"connector_action_requests", "history_entries", "audit_outbox"} {
					var count int
					if err := fixture.database.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
						t.Fatalf("failed admission left %s rows=%d: %v", table, count, err)
					}
				}
			} else {
				stored, err := fixture.store.GetActionRequest(t.Context(), persistence.RequestID)
				if err != nil || stored.Status != connectors.ResultRunning || stored.DispatchStartedAt == "" {
					t.Fatalf("terminal audit failure rewrote durable dispatched state: %#v %v", stored, err)
				}
				fixture.historyStatus(t, stored.ID, connectors.ResultRunning)
				replay, err := fixture.runtime.Call(t.Context(), fixture.call)
				if err != nil || !replay.Replayed || replay.Result.Status != connectors.ResultRunning || fixture.connector.dispatches.Load() != 1 {
					t.Fatalf("failed terminal persistence redispatched: %#v %v", replay, err)
				}
			}
			if _, err := fixture.database.ExecContext(t.Context(), `DROP TRIGGER fail_native_workflow_audit`); err != nil {
				t.Fatal(err)
			}
			if terminal {
				fixture.runtime.Recover(t.Context(), time.Now().Add(time.Hour))
			}
			result, err := fixture.runtime.Call(t.Context(), fixture.call)
			want := connectors.ResultCompleted
			if terminal {
				want = connectors.ResultOutcomeUnknown
			}
			if err != nil || result.Result.Status != want || result.Replayed != terminal || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("healthy readmission/recovery finality: %#v %v", result, err)
			}
			fixture.historyStatus(t, result.Request.ID, want)
		})
	}
}
