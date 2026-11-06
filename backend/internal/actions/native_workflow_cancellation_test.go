package actions

import (
	"context"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeWorkflowCanceledMutationPersistsUnknownWithoutRedispatch(t *testing.T) {
	for _, rule := range []connectortargets.ActionPermissionRule{connectortargets.ActionPermissionAlwaysRun, connectortargets.ActionPermissionApprovalRequired} {
		t.Run(string(rule), func(t *testing.T) {
			fixture := newNativeWorkflowFixture(t, rule, connectors.RiskWrite)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			fixture.connector.execute = func(executionContext context.Context, _ connectors.RuntimeContext, _ connectors.PreparedAction) (connectors.ActionResult, error) {
				cancel()
				if executionContext.Err() != context.Canceled {
					t.Error("connector execution detached from caller cancellation")
				}
				return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown("caller_canceled", nil, context.Canceled)
			}
			started := time.Now()
			result, err := fixture.runtime.Call(ctx, fixture.call)
			if err != nil {
				t.Fatal(err)
			}
			if rule == connectortargets.ActionPermissionApprovalRequired {
				result.Request, err = fixture.runtime.RunPending(ctx, result.Request.ID, "Approve owned cancellation fixture")
			}
			if err != nil || result.Request.Status != connectors.ResultOutcomeUnknown || time.Since(started) > 5*time.Second {
				t.Fatalf("canceled mutation finalization: %#v %v", result.Request, err)
			}
			fixture.historyStatus(t, result.Request.ID, connectors.ResultOutcomeUnknown)
			replay, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || !replay.Replayed || replay.Result.Status != connectors.ResultOutcomeUnknown || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("canceled mutation redispatched: %#v %v calls=%d", replay, err, fixture.connector.dispatches.Load())
			}
		})
	}
}
