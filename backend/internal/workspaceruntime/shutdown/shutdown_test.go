package shutdown

import (
	"context"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type actionWorkflowSpy struct {
	stopped bool
	message string
}

type shutdownRuntimeSpy struct {
	*workspaceruntime.Runtime
	cleared chan struct{}
}

func (runtime *shutdownRuntimeSpy) ClearActionIdentity() {
	runtime.Runtime.ClearActionIdentity()
	close(runtime.cleared)
}

func TestCloseDefersStorageCleanupUntilRegisteredComponentsDrain(t *testing.T) {
	concrete := &workspaceruntime.Runtime{ID: "workspace-pending", ActionIdentityKey: make([]byte, 32)}
	runtime := &shutdownRuntimeSpy{Runtime: concrete, cleared: make(chan struct{})}
	release := make(chan struct{})
	waiting := make(chan struct{})
	key := componentstate.NewKey[*struct{}]("pending-component")
	if err := componentstate.RegisterLifecycle(runtime.ComponentStatePort(), key, componentstate.Lifecycle{
		Name: "pending-component",
		Close: func(context.Context) (bool, error) {
			return false, nil
		},
		Abort: func() {},
		Wait: func(context.Context) bool {
			close(waiting)
			<-release
			return true
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := Close(runtime, nil, nil); err == nil {
		t.Fatal("pending component shutdown did not report deferred storage close")
	}
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("deferred component wait did not start")
	}
	if concrete.ActionIdentityKey == nil {
		t.Fatal("storage cleanup ran before the component drained")
	}
	close(release)
	select {
	case <-runtime.cleared:
	case <-time.After(time.Second):
		t.Fatal("storage cleanup did not run after the component drained")
	}
}

func TestDiscardAbortsRegisteredComponents(t *testing.T) {
	runtime := &workspaceruntime.Runtime{ID: "opening", ActionIdentityKey: make([]byte, 32)}
	aborted := false
	key := componentstate.NewKey[*struct{}]("opening-component")
	if err := componentstate.RegisterLifecycle(runtime.ComponentStatePort(), key, componentstate.Lifecycle{
		Name:  "opening-component",
		Close: func(context.Context) (bool, error) { return true, nil },
		Abort: func() { aborted = true },
		Wait:  func(context.Context) bool { return true },
	}); err != nil {
		t.Fatal(err)
	}
	if err := Discard(runtime); err != nil {
		t.Fatal(err)
	}
	if !aborted {
		t.Fatal("discard did not abort the registered component")
	}
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
