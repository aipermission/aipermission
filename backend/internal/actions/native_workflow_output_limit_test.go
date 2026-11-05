package actions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestNativeWorkflowOversizedRedactionPersistsUnknownWithoutReplay(t *testing.T) {
	fixture := newNativeWorkflowFixtureWithSecret(t, connectortargets.ActionPermissionAlwaysRun, connectors.RiskWrite, "tiny")
	fixture.connector.actions[0].OutputHint.FinalMaxBytes = 200
	fixture.connector.execute = func(context.Context, connectors.RuntimeContext, connectors.PreparedAction) (connectors.ActionResult, error) {
		// The raw output fits. Mandatory credential redaction expands it.
		return connectors.ActionResult{Status: connectors.ResultCompleted, Output: strings.Repeat("tiny ", 30)}, nil
	}
	for attempt := range 2 {
		result, err := fixture.runtime.Call(t.Context(), fixture.call)
		if err != nil || result.Result.Status != connectors.ResultOutcomeUnknown || result.Replayed != (attempt == 1) || fixture.connector.dispatches.Load() != 1 {
			t.Fatalf("oversized dispatched outcome/replay: %#v %v calls=%d", result, err, fixture.connector.dispatches.Load())
		}
		stored, err := fixture.store.GetActionRequest(t.Context(), result.Request.ID)
		if err != nil || stored.Status != connectors.ResultOutcomeUnknown || stored.Output != nil {
			t.Fatalf("oversized data persisted: %#v %v", stored, err)
		}
		var output string
		if err := fixture.database.QueryRowContext(t.Context(), `SELECT output_json FROM history_entries WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`, stored.ID).Scan(&output); err != nil {
			t.Fatal(err)
		}
		if len(output) > 200 || strings.Contains(output, "tiny") || strings.Contains(output, actionresult.CredentialRedactionMarker) {
			t.Fatalf("history retained rejected oversized output: %s", output)
		}
		encoded, _ := json.Marshal(result.Result.Output)
		if len(encoded) > 200 {
			t.Fatalf("public response escaped final byte cap: %s", encoded)
		}
		fixture.historyStatus(t, stored.ID, connectors.ResultOutcomeUnknown)
	}
}
