package actions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeWorkflowPendingApprovalDeclineAndSingleDispatch(t *testing.T) {
	for _, decline := range []bool{false, true} {
		t.Run(map[bool]string{false: "approve", true: "decline"}[decline], func(t *testing.T) {
			fixture := newNativeWorkflowFixture(t, connectortargets.ActionPermissionApprovalRequired, connectors.RiskRead)
			pending, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || pending.Result.Status != connectors.ResultApprovalPending || fixture.connector.dispatches.Load() != 0 {
				t.Fatalf("pending admission: %#v %v", pending, err)
			}
			preview, err := fixture.runtime.ApprovalPreview(t.Context(), pending.Request)
			if err != nil || preview == nil {
				t.Fatalf("actual sealed preview: %#v %v", preview, err)
			}
			var finished connectortargets.ActionRequest
			want, dispatches := connectors.ResultCompleted, int64(1)
			if decline {
				want, dispatches = connectors.ResultDeclined, 0
				finished, err = fixture.runtime.DeclinePending(t.Context(), pending.Request.ID, "Operator declined")
			} else {
				finished, err = fixture.runtime.RunPending(t.Context(), pending.Request.ID, "Operator approved")
			}
			if err != nil || finished.Status != want || fixture.connector.dispatches.Load() != dispatches {
				t.Fatalf("approval transition: %#v %v calls=%d", finished, err, fixture.connector.dispatches.Load())
			}
			fixture.historyStatus(t, finished.ID, want)
			if _, err := fixture.runtime.RunPending(t.Context(), pending.Request.ID, ""); !errors.Is(err, connectortargets.ErrActionRequestNotPending) {
				t.Fatalf("terminal approval rerun: %v", err)
			}
			replayed, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || !replayed.Replayed || replayed.Request.ID != pending.Request.ID || replayed.Result.Status != want || fixture.connector.dispatches.Load() != dispatches {
				t.Fatalf("same-key replay dispatched: %#v %v", replayed, err)
			}
		})
	}
}

func TestNativeWorkflowUnknownOutcomeAndMandatoryCredentialBoundary(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "unknown"}[unknown], func(t *testing.T) {
			fixture := newNativeWorkflowFixture(t, connectortargets.ActionPermissionAlwaysRun, connectors.RiskWrite)
			withoutKey := fixture.call
			withoutKey.IdempotencyKey = ""
			if _, err := fixture.runtime.Call(t.Context(), withoutKey); err == nil || !strings.Contains(err.Error(), "idempotency_key is required") || fixture.connector.dispatches.Load() != 0 {
				t.Fatalf("mutation admitted without stable key: %v", err)
			}
			redactor, err := actionresult.NewRedactor(
				func(_ context.Context, value string) string {
					return strings.ReplaceAll(value, "private", "optional-change")
				},
				func(_ context.Context, value string) string { return value }, 64<<10,
			)
			if err != nil {
				t.Fatal(err)
			}
			fixture.runtime.redactor = redactor
			fixture.connector.execute = func(ctx context.Context, runtime connectors.RuntimeContext, _ connectors.PreparedAction) (connectors.ActionResult, error) {
				secret, err := runtime.Secrets.GetSecret(ctx, "password")
				if err != nil || secret != nativeWorkflowSecret {
					t.Errorf("actual sealed credential unavailable: %v", err)
				}
				if unknown {
					return connectors.ActionResult{}, connectors.ClassifyOutcomeUnknown("response_timeout", nil, errors.New("reply lost"))
				}
				return connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"text": secret, "safe": "visible"}, DisplayText: secret, Error: secret}, nil
			}
			result, err := fixture.runtime.Call(t.Context(), fixture.call)
			want := connectors.ResultCompleted
			if unknown {
				want = connectors.ResultOutcomeUnknown
			}
			if err != nil || result.Result.Status != want || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("dispatch: %#v %v", result, err)
			}
			stored, err := fixture.store.GetActionRequest(t.Context(), result.Request.ID)
			if err != nil || stored.Status != want {
				t.Fatalf("durable terminal result: %#v %v", stored, err)
			}
			for _, value := range []any{result.Result, stored.Output, stored.DisplayText, stored.Error} {
				encoded, err := json.Marshal(value)
				if err != nil || strings.Contains(string(encoded), nativeWorkflowSecret) {
					t.Fatalf("credential reached public terminal projection: %s %v", encoded, err)
				}
			}
			if !unknown {
				output, ok := result.Result.Output.(map[string]any)
				if !ok || output["safe"] != "visible" || output["text"] != actionresult.CredentialRedactionMarker || result.Result.DisplayText != actionresult.CredentialRedactionMarker || result.Result.Error != actionresult.CredentialRedactionMarker {
					t.Fatalf("mandatory boundary was weakened by optional transform or lost safe output: %#v", result.Result)
				}
				storedOutput, ok := stored.Output.(map[string]any)
				if !ok || storedOutput["safe"] != "visible" || storedOutput["text"] != actionresult.CredentialRedactionMarker || stored.DisplayText != actionresult.CredentialRedactionMarker || stored.Error != actionresult.CredentialRedactionMarker {
					t.Fatalf("durable result lost mandatory boundary or safe data: %#v", stored)
				}
				var outputJSON, outputText, errorText string
				if err := fixture.database.QueryRowContext(t.Context(), `SELECT output_json, output_text, error FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, stored.ID).Scan(&outputJSON, &outputText, &errorText); err != nil {
					t.Fatal(err)
				}
				var historyOutput map[string]any
				if err := json.Unmarshal([]byte(outputJSON), &historyOutput); err != nil || historyOutput["safe"] != "visible" || historyOutput["text"] != actionresult.CredentialRedactionMarker || outputText != actionresult.CredentialRedactionMarker || errorText != actionresult.CredentialRedactionMarker {
					t.Fatalf("history projection lost mandatory boundary/safe data: %s %q %q %v", outputJSON, outputText, errorText, err)
				}
			}
			fixture.historyStatus(t, stored.ID, want)
			replay, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || !replay.Replayed || replay.Result.Status != want || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("terminal outcome redispatched: %#v %v", replay, err)
			}
			changed := fixture.call
			changed.Input = map[string]any{"sql": "select 2"}
			if _, err := fixture.runtime.Call(t.Context(), changed); !errors.Is(err, connectortargets.ErrActionRequestIdempotency) || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("same-key changed input accepted: %v", err)
			}
		})
	}
}
