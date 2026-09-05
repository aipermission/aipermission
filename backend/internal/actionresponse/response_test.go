package actionresponse

import (
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestFromRequestPreservesCanonicalStateAndRetryGuidance(t *testing.T) {
	request := connectortargets.ActionRequest{
		ID: 42, Status: connectors.ResultOutcomeUnknown,
		ConnectorKind: "example", TargetID: 7, ProfileID: 8, ActionName: "write",
		Input: map[string]any{"value": "safe"},
	}
	response := FromResult(request, connectors.ActionResult{
		Status: connectors.ResultCompleted, Output: map[string]any{"accepted": true},
	}, "ignored")
	if response.Status != string(connectors.ResultOutcomeUnknown) || response.TargetRef != "example:7:8" {
		t.Fatalf("canonical projection drifted: %#v", response)
	}
	if response.RetryAfterSeconds != 0 || !strings.Contains(response.AssistantHint, "Do not retry") {
		t.Fatalf("outcome-unknown guidance drifted: %#v", response)
	}
}

func TestFromRequestUsesConnectorRunningHint(t *testing.T) {
	response := FromRequest(connectortargets.ActionRequest{
		Status: connectors.ResultRunning, ConnectorKind: "example", ActionName: "read",
	}, "  poll with connector recovery  ")
	if response.RetryAfterSeconds != 3 || response.AssistantHint != "poll with connector recovery" {
		t.Fatalf("running response = %#v", response)
	}
}

func TestFromRequestAddsApprovalPollingGuidance(t *testing.T) {
	response := FromRequest(connectortargets.ActionRequest{
		Status: connectors.ResultApprovalPending, ConnectorKind: "example", ActionName: "write",
	}, "")
	if response.RetryAfterSeconds != 3 || response.AssistantHint != ApprovalPendingHint {
		t.Fatalf("approval response = %#v", response)
	}
}

func TestWithholdRemovesContentAndPreservesSafetyGuidance(t *testing.T) {
	response := Response{
		Input: map[string]any{"secret": "input"}, Output: "secret output",
		DisplayText: "secret display", Error: "secret error", AssistantHint: OutcomeUnknownHint,
	}
	Withhold(&response)
	if response.Input != nil || response.Output != nil || response.DisplayText != "" || response.Error != "" || !response.OutputWithheld {
		t.Fatalf("content was not withheld: %#v", response)
	}
	if !strings.Contains(response.AssistantHint, "Do not retry") || !strings.Contains(response.AssistantHint, "authorization") {
		t.Fatalf("safety guidance was not preserved: %q", response.AssistantHint)
	}
	firstHint := response.AssistantHint
	Withhold(&response)
	if response.AssistantHint != firstHint {
		t.Fatalf("withholding was not idempotent: %q", response.AssistantHint)
	}
}
