package gatewayconnectoractions

import (
	"errors"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
)

func TestWorkflowRejectsIncompleteWorkspace(t *testing.T) {
	component := New(Dependencies{SupportsRunning: func(actions.PreparedRequest) bool { return false }})
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
