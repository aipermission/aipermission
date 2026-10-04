package gatewayconnectoractions

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/mcpconnector"
)

func TestMCPResponseProjectionsStayIdentical(t *testing.T) {
	for _, status := range []connectors.ResultStatus{connectors.ResultRunning, connectors.ResultCompleted, connectors.ResultFailed} {
		request := connectortargets.ActionRequest{
			ID: 42, Status: status, ConnectorKind: "fixture", TargetID: 3, ProfileID: 4,
			ActionName: "inspect", Output: map[string]any{"ok": true},
		}
		result := connectors.ActionResult{Status: status, Output: map[string]any{"ok": true}}
		resolve := func(connectortargets.ActionRequest) string { return "  poll later  " }
		gateway := MCPResponseFromResult(request, result, resolve)
		mcp := mcpconnector.ResponseFromResult(resolve, request, result)
		gatewayJSON, err := json.Marshal(gateway)
		if err != nil {
			t.Fatal(err)
		}
		mcpJSON, err := json.Marshal(mcp)
		if err != nil {
			t.Fatal(err)
		}
		if string(gatewayJSON) != string(mcpJSON) {
			t.Fatalf("status %s projected differently: %s / %s", status, gatewayJSON, mcpJSON)
		}
	}
}

func TestWrapResponsePreservesEveryCanonicalField(t *testing.T) {
	canonical := actions.Response{
		Status: "outcome_unknown", RequestID: 9007199254740993, TargetRef: "fixture:3:4",
		TargetName: "caf\u00e9", ConnectorKind: "fixture", ProfileLabel: "selected", ActionName: "inspect",
		Input:  map[string]any{"offset": json.Number("9223372036854775807")},
		Output: map[string]any{"count": json.Number("9007199254740993")}, DisplayText: "inspect first",
		Error: "safe error", RetryPolicy: *connectors.ConditionalRetryPolicy("expected_version"),
		RetryAfterSeconds: 3, AssistantHint: "do not retry", OutputWithheld: true, Replayed: true,
	}
	actual := wrapResponse(canonical)
	if !reflect.DeepEqual(actions.Response(actual), canonical) {
		t.Fatalf("canonical fields lost: %#v", actual)
	}
	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != string(expected) {
		t.Fatalf("wire projection drifted: %s / %s", encoded, expected)
	}
}
