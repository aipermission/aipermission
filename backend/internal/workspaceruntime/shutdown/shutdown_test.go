package shutdown

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"testing"
	"time"

	workspacestorage "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type actionWorkflowSpy struct {
	stopped  bool
	message  string
	shutdown func(context.Context) error
}

func TestCloseDefersStorageCleanupUntilRegisteredComponentsDrain(t *testing.T) {
	storageClosed := make(chan struct{})
	database := sql.OpenDB(closeSignalConnector{closed: storageClosed})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := &workspaceruntime.Runtime{
		ID: "workspace-pending", ActionIdentityKey: make([]byte, 32),
		Storage: workspacestorage.New(database, nil, nil, "workspace-pending", nil),
	}
	release := make(chan struct{})
	waiting := make(chan struct{})
	transfer := &transferWorkflowSpy{wait: func(context.Context) bool {
		close(waiting)
		<-release
		return true
	}}
	if err := Close(runtime, nil, nil, func() TransferWorkflow { return transfer }); err == nil {
		t.Fatal("pending component shutdown did not report deferred storage close")
	}
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("deferred component wait did not start")
	}
	if runtime.ActionIdentityKey != nil {
		t.Fatal("action identity key remained available while transfers drained")
	}
	select {
	case <-storageClosed:
		t.Fatal("database closed before the transfer component drained")
	default:
	}
	close(release)
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("storage cleanup did not run after the component drained")
	}
	if runtime.ActionIdentityKey != nil {
		t.Fatal("action identity key was retained after storage cleanup")
	}
}

type closeSignalConnector struct{ closed chan struct{} }

func (connector closeSignalConnector) Connect(context.Context) (driver.Conn, error) {
	return &closeSignalConnection{closed: connector.closed}, nil
}

func (connector closeSignalConnector) Driver() driver.Driver {
	return closeSignalDriver{closed: connector.closed}
}

type closeSignalDriver struct{ closed chan struct{} }

func (driver closeSignalDriver) Open(string) (driver.Conn, error) {
	return &closeSignalConnection{closed: driver.closed}, nil
}

type closeSignalConnection struct{ closed chan struct{} }

func (*closeSignalConnection) Prepare(string) (driver.Stmt, error) { return nil, driver.ErrSkip }
func (connection *closeSignalConnection) Close() error {
	close(connection.closed)
	return nil
}
func (*closeSignalConnection) Begin() (driver.Tx, error)  { return nil, driver.ErrSkip }
func (*closeSignalConnection) Ping(context.Context) error { return nil }

func TestDiscardAbortsRegisteredComponents(t *testing.T) {
	runtime := &workspaceruntime.Runtime{ID: "opening", ActionIdentityKey: make([]byte, 32)}
	transfer := &transferWorkflowSpy{}
	if err := Discard(runtime, func() TransferWorkflow { return transfer }); err != nil {
		t.Fatal(err)
	}
	if !transfer.aborted {
		t.Fatal("discard did not abort the registered component")
	}
}

type transferWorkflowSpy struct {
	aborted bool
	wait    func(context.Context) bool
}

func (*transferWorkflowSpy) Shutdown(time.Duration, string, string) (bool, bool, error) {
	return true, false, nil
}
func (workflow *transferWorkflowSpy) Wait(ctx context.Context) bool {
	if workflow.wait != nil {
		return workflow.wait(ctx)
	}
	return true
}
func (workflow *transferWorkflowSpy) Abort() { workflow.aborted = true }

type commandWorkflowSpy struct {
	message string
	stop    func(context.Context) error
}

func (workflow *commandWorkflowSpy) StopWorkers(ctx context.Context) error {
	if workflow.stop != nil {
		return workflow.stop(ctx)
	}
	return nil
}

func (workflow *commandWorkflowSpy) CancelRunning(_ context.Context, message string) error {
	workflow.message = message
	return nil
}

func (workflow *actionWorkflowSpy) Shutdown(ctx context.Context) error {
	workflow.stopped = true
	if workflow.shutdown != nil {
		return workflow.shutdown(ctx)
	}
	return nil
}

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
	}, nil); err != nil {
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
	if err := Discard(runtime, nil); err != nil {
		t.Fatal(err)
	}
	if runtime.ActionIdentityKey != nil {
		t.Fatal("discard retained action identity key")
	}
}

func TestCloseDefersStorageWhileActionAndCommandWorkersDrain(t *testing.T) {
	storageClosed := make(chan struct{})
	database := sql.OpenDB(closeSignalConnector{closed: storageClosed})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := &workspaceruntime.Runtime{
		ID: "workspace-workers", ActionIdentityKey: make([]byte, 32),
		Storage: workspacestorage.New(database, nil, nil, "workspace-workers", nil),
	}
	actionRelease := make(chan struct{})
	commandRelease := make(chan struct{})
	actionCalls := 0
	commandCalls := 0
	actions := &actionWorkflowSpy{shutdown: func(ctx context.Context) error {
		actionCalls++
		if actionCalls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		<-actionRelease
		return nil
	}}
	commands := &commandWorkflowSpy{stop: func(ctx context.Context) error {
		commandCalls++
		if commandCalls == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		<-commandRelease
		return nil
	}}
	err := closeWithTimeout(runtime,
		func() (ActionWorkflow, error) { return actions, nil },
		func() (CommandWorkflow, error) { return commands, nil }, nil, 10*time.Millisecond)
	if err == nil {
		t.Fatal("worker drain timeout did not defer storage close")
	}
	if runtime.ActionIdentityKey == nil {
		t.Fatal("action identity key cleared while an action finalizer could still use it")
	}
	select {
	case <-storageClosed:
		t.Fatal("database closed while workspace workers were active")
	default:
	}
	close(actionRelease)
	select {
	case <-storageClosed:
		t.Fatal("database closed before command workers drained")
	case <-time.After(20 * time.Millisecond):
	}
	close(commandRelease)
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("database did not close after every workspace worker drained")
	}
	if runtime.ActionIdentityKey != nil {
		t.Fatal("action identity key remained after finalizers drained")
	}
}
