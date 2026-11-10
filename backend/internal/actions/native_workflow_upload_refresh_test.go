package actions

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeUploadPollRefreshPreservesFinalityAndReadFailures(t *testing.T) {
	readFailure := errors.New("stage readiness unavailable")
	for _, test := range []struct {
		name        string
		ready       bool
		readErr     error
		raceRunning bool
		want        connectors.ResultStatus
	}{
		{name: "ready", ready: true, want: connectors.ResultApprovalPending},
		{name: "expired", want: connectors.ResultStale},
		{name: "read-failure", readErr: readFailure, want: connectors.ResultApprovalPending},
		{name: "running-race", raceRunning: true, want: connectors.ResultRunning},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newNativeWorkflowFixture(t, connectortargets.ActionPermissionApprovalRequired, connectors.RiskWrite)
			f.connector.actions[0].MaxUploadBytes = 1024
			pending, err := f.runtime.Call(connectors.WithStagedUpload(t.Context()), f.call)
			if err != nil || pending.Request.Status != connectors.ResultApprovalPending {
				t.Fatalf("upload approval: %#v %v", pending, err)
			}
			f.runtime.uploadReady = func(ctx context.Context, approval connectors.FileUploadApproval) (bool, error) {
				if !f.delivery.held() || approval.RequestID != pending.Request.ID || approval.TargetRef != f.call.TargetRef || approval.TokenID != f.call.TokenID {
					t.Fatalf("readiness lost admission or identity: %#v", approval)
				}
				if test.raceRunning {
					if _, err := f.store.MarkActionRequestRunning(ctx, pending.Request.ID, "concurrent-operator", time.Now().Add(time.Minute)); err != nil {
						t.Fatal(err)
					}
				}
				return test.ready, test.readErr
			}
			updated, err := f.runtime.RefreshRequest(t.Context(), pending.Request)
			if !errors.Is(err, test.readErr) || updated.Status != test.want {
				t.Fatalf("poll result: status=%s error=%v; want status=%s error=%v", updated.Status, err, test.want, test.readErr)
			}
			stored, err := f.store.GetActionRequest(t.Context(), pending.Request.ID)
			if err != nil || stored.Status != test.want || f.connector.dispatches.Load() != 0 || f.delivery.held() {
				t.Fatalf("poll changed execution or leaked admission: %#v %v", stored, err)
			}
			if test.want == connectors.ResultStale {
				// Reconcile an already-read pending record again: only one transition
				// and audit may commit even when callers hold the old snapshot.
				if _, err := f.runtime.RefreshRequest(t.Context(), pending.Request); err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err := f.database.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM audit_outbox WHERE action = 'connector_action.request.stale'`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			want := 0
			if test.want == connectors.ResultStale {
				want = 1
			}
			if count != want {
				t.Fatalf("stale audit count=%d want=%d", count, want)
			}
			if test.want != connectors.ResultApprovalPending {
				f.historyStatus(t, stored.ID, test.want)
			}
		})
	}
}
