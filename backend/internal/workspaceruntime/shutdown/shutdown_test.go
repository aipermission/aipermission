package shutdown

import (
	"context"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type actionWorkflowSpy struct {
	stopped bool
	message string
}

type commandWorkflowSpy struct{ message string }

func (workflow *commandWorkflowSpy) CancelRunning(_ context.Context, message string) error {
	workflow.message = message
	return nil
}

func (workflow *actionWorkflowSpy) StopRecovery() { workflow.stopped = true }

func (workflow *actionWorkflowSpy) MarkRunningOutcomeUnknown(_ context.Context, message string) error {
	workflow.message = message
	return nil
}

func TestCloseResolvesAndStopsConnectorActions(t *testing.T) {
	runtime := &workspaceruntime.Runtime{ID: "workspace-one", ActionIdentityKey: make([]byte, 32)}
	workflow := &actionWorkflowSpy{}
	commands := &commandWorkflowSpy{}
	resolveCalls := 0
	if err := Close(runtime, func() (ActionWorkflow, error) {
		resolveCalls++
		return workflow, nil
	}, func() (CommandWorkflow, error) {
		return commands, nil
	}); err != nil {
		t.Fatal(err)
	}
	if resolveCalls != 1 || !workflow.stopped {
		t.Fatalf("resolve calls=%d stopped=%v", resolveCalls, workflow.stopped)
	}
	if workflow.message != runtimeoutcome.ConnectorActionUnknown {
		t.Fatalf("outcome message=%q", workflow.message)
	}
	if commands.message != runtimeoutcome.CommandCanceled {
		t.Fatalf("command outcome message=%q", commands.message)
	}
	if runtime.ActionIdentityKey != nil {
		t.Fatal("action identity key was retained")
	}
}

func TestDiscardDoesNotRunActionRecovery(t *testing.T) {
	runtime := &workspaceruntime.Runtime{ID: "opening", ActionIdentityKey: make([]byte, 32)}
	if err := Discard(runtime); err != nil {
		t.Fatal(err)
	}
	if runtime.ActionIdentityKey != nil {
		t.Fatal("discard retained action identity key")
	}
}
