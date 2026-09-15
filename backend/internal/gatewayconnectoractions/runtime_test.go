package gatewayconnectoractions

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func TestWorkflowRejectsIncompleteWorkspace(t *testing.T) {
	component := New(Dependencies{SupportsRunning: func(PreparedRequest) bool { return false }})
	if _, err := component.workflow(Workspace{}); !errors.Is(err, actions.ErrWorkflowUnavailable) {
		t.Fatalf("Workflow() error = %v, want ErrWorkflowUnavailable", err)
	}
}

func TestRedactorRejectsIncompleteWorkspace(t *testing.T) {
	if _, err := New(Dependencies{}).redactor(Workspace{}); !errors.Is(err, actions.ErrWorkflowUnavailable) {
		t.Fatalf("Redactor() error = %v, want ErrWorkflowUnavailable", err)
	}
}

func TestStopRecoveryAcceptsWorkspaceWithoutState(t *testing.T) {
	New(Dependencies{}).StopRecovery(Workspace{})
}

func TestMCPResponseUsesPersistedStatusAndRunningHint(t *testing.T) {
	request := connectortargets.ActionRequest{
		ID: 71, Status: connectors.ResultRunning, ActionName: "inspect", ConnectorKind: "fixture",
	}
	result := connectors.ActionResult{Status: connectors.ResultCompleted, Output: map[string]any{"count": 2}}

	response := MCPResponseFromResult(request, result, func(connectortargets.ActionRequest) string { return " poll action 71 " })
	if response.Status != string(connectors.ResultRunning) || response.RequestID != 71 || response.AssistantHint != "poll action 71" || response.RetryAfterSeconds != 3 {
		t.Fatalf("running response = %#v", response)
	}
	if output, ok := response.Output.(map[string]any); !ok || output["count"] != 2 {
		t.Fatalf("running result output = %#v", response.Output)
	}

	response = MCPResponseFromResult(request, result, func(connectortargets.ActionRequest) string { return " " })
	if response.AssistantHint == "" || response.AssistantHint == "poll action 71" {
		t.Fatalf("missing safe fallback hint: %#v", response)
	}
	request.Status = connectors.ResultCompleted
	response = MCPResponseFromResult(request, result, func(connectortargets.ActionRequest) string {
		t.Fatal("completed requests must not resolve a running hint")
		return ""
	})
	if response.Status != string(connectors.ResultCompleted) || response.RetryAfterSeconds != 0 {
		t.Fatalf("completed response = %#v", response)
	}
}
