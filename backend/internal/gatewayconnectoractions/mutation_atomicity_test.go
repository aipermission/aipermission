package gatewayconnectoractions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/transactionstate"
)

func TestActionAuditFailureRollsBackAdmissionBeforeDispatch(t *testing.T) {
	fixture := newApprovalFixture(t)
	fixture.permission(t, connectortargets.ActionPermissionAlwaysRun)
	fixture.call.IdempotencyKey = "audit-admission-retry"
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	database := fixture.workspace.Storage.Database
	if _, err := database.ExecContext(ctx, `
		CREATE TRIGGER reject_action_admission_audit BEFORE INSERT ON audit_outbox
		WHEN NEW.action = 'connector_action.request.created'
		BEGIN SELECT RAISE(ABORT, 'injected action admission audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.component.Call(ctx, fixture.workspace, fixture.call)
	if err == nil || !transactionstate.IsNotCommitted(err) || !strings.Contains(err.Error(), "injected action admission audit failure") ||
		result.Request.ID != 0 || fixture.connector.dispatches.Load() != 0 {
		t.Fatalf("failed audit did not fence admission: result=%#v err=%v dispatches=%d", result, err, fixture.connector.dispatches.Load())
	}
	for _, table := range []string{"connector_action_requests", "history_entries", "audit_outbox", "message_queue"} {
		var count int
		if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("audit failure left %d rows in %s", count, table)
		}
	}
	if _, err := database.ExecContext(ctx, "DROP TRIGGER reject_action_admission_audit"); err != nil {
		t.Fatal(err)
	}
	// A proven rollback leaves the same key available; only this later admitted
	// call may dispatch. This does not authorize retries after an unknown outcome.
	result, err = fixture.component.Call(ctx, fixture.workspace, fixture.call)
	if err != nil || result.Request.Status != connectors.ResultCompleted || result.Replayed || fixture.connector.dispatches.Load() != 1 {
		t.Fatalf("healthy admission after rollback = %#v, err=%v dispatches=%d", result, err, fixture.connector.dispatches.Load())
	}
	var requests, history, audits int
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM connector_action_requests").Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM history_entries").Scan(&history); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector_action.request.created'").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || history != 1 || audits != 1 {
		t.Fatalf("healthy admission did not preserve domain/history/audit: requests=%d history=%d audits=%d", requests, history, audits)
	}
}
