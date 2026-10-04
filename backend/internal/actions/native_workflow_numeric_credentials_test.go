package actions

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeWorkflowNumericCredentialNeverReachesResultsOrHistory(t *testing.T) {
	for _, secret := range []string{"7", "1234567890123", "9007199254740993", "-12345", "1.25", "1e3"} {
		t.Run(secret, func(t *testing.T) {
			fixture := newNativeWorkflowFixtureWithSecret(t, connectortargets.ActionPermissionAlwaysRun, connectors.RiskRead, secret)
			fixture.connector.execute = func(ctx context.Context, runtime connectors.RuntimeContext, _ connectors.PreparedAction) (connectors.ActionResult, error) {
				actual, err := runtime.Secrets.GetSecret(ctx, "password")
				if err != nil || actual != secret {
					return connectors.ActionResult{}, errors.New("sealed fixture credential unavailable or changed")
				}
				return connectors.ActionResult{Status: connectors.ResultCompleted,
					Output:      map[string]any{"value": json.Number(actual), "safe": "visible", "native": 42, "nested": []any{json.Number(actual)}},
					DisplayText: actual, Metadata: map[string]any{"value": json.Number(actual)},
				}, nil
			}
			result, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || result.Result.Status != connectors.ResultCompleted || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("dispatch: %#v err=%v", result, err)
			}
			stored, err := fixture.store.GetActionRequest(t.Context(), result.Request.ID)
			if err != nil || stored.Status != connectors.ResultCompleted {
				t.Fatalf("stored result: %#v err=%v", stored, err)
			}
			var historyJSON string
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT output_json FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, stored.ID).Scan(&historyJSON); err != nil {
				t.Fatal(err)
			}
			var history map[string]any
			if err := json.Unmarshal([]byte(historyJSON), &history); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"value": actionresult.CredentialRedactionMarker, "safe": "visible", "native": float64(42), "nested": []any{actionresult.CredentialRedactionMarker}}
			for _, output := range []any{result.Result.Output, stored.Output, history} {
				encoded, err := json.Marshal(output)
				if err != nil {
					t.Fatal(err)
				}
				var normalized map[string]any
				if err := json.Unmarshal(encoded, &normalized); err != nil || !reflect.DeepEqual(normalized, want) {
					t.Fatalf("public or durable output differs from safe projection: %s err=%v", encoded, err)
				}
			}
			if result.Result.Metadata["value"] != actionresult.CredentialRedactionMarker || result.Result.DisplayText != actionresult.CredentialRedactionMarker {
				t.Fatalf("delivery metadata/text exposed credential: %#v", result.Result)
			}
			fixture.historyStatus(t, stored.ID, connectors.ResultCompleted)
			replay, err := fixture.runtime.Call(t.Context(), fixture.call)
			if err != nil || !replay.Replayed || replay.Request.ID != stored.ID || fixture.connector.dispatches.Load() != 1 {
				t.Fatalf("same-key replay redispatched: %#v err=%v", replay, err)
			}
			encoded, err := json.Marshal(replay.Result.Output)
			var replayOutput map[string]any
			if err != nil || json.Unmarshal(encoded, &replayOutput) != nil || !reflect.DeepEqual(replayOutput, want) || replay.Result.DisplayText != actionresult.CredentialRedactionMarker {
				t.Fatalf("replay exposed numeric credential: %s err=%v", encoded, err)
			}
		})
	}
}
