package shutdown

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/runtimeoutcome"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	workspacestorage "github.com/aipermission/aipermission/backend/internal/workspaceruntime/state/storage"
)

type actionWorkflowSpy struct {
	stopped  bool
	begin    func()
	message  string
	shutdown func(context.Context) error
}

func (workflow *actionWorkflowSpy) BeginShutdown() {
	workflow.stopped = true
	if workflow.begin != nil {
		workflow.begin()
	}
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
	if err := closeWithTimeout(runtime, nil, nil, func() TransferWorkflow { return transfer }, 10*time.Millisecond); err == nil {
		t.Fatal("pending component shutdown did not report deferred storage close")
	}
	select {
	case <-waiting:
	case <-time.After(time.Second):
		t.Fatal("deferred component wait did not start")
	}
	if runtime.HasActionIdentity() {
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
	if runtime.HasActionIdentity() {
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
	if err := Discard(runtime, func() TransferWorkflow { return transfer }, nil); err != nil {
		t.Fatal(err)
	}
	if !transfer.aborted {
		t.Fatal("discard did not abort the registered component")
	}
}

type transferWorkflowSpy struct {
	aborted     bool
	begin       func()
	beginResult func() (bool, error)
	wait        func(context.Context) bool
	recover     func(context.Context) error
}

func (workflow *transferWorkflowSpy) BeginShutdown() (bool, error) {
	if workflow.begin != nil {
		workflow.begin()
	}
	if workflow.beginResult != nil {
		return workflow.beginResult()
	}
	return true, nil
}
func (workflow *transferWorkflowSpy) Wait(ctx context.Context) bool {
	if workflow.wait != nil {
		return workflow.wait(ctx)
	}
	return true
}
func (workflow *transferWorkflowSpy) Recover(ctx context.Context, _, _ string) error {
	if workflow.recover != nil {
		return workflow.recover(ctx)
	}
	return nil
}
func (workflow *transferWorkflowSpy) Abort() { workflow.aborted = true }

type commandWorkflowSpy struct {
	message string
	begin   func()
	stop    func(context.Context) error
}

func (workflow *commandWorkflowSpy) BeginWorkerShutdown() {
	if workflow.begin != nil {
		workflow.begin()
	}
}

func (workflow *commandWorkflowSpy) WaitWorkers(ctx context.Context) error {
	if workflow.stop != nil {
		return workflow.stop(ctx)
	}
	return nil
}

func (workflow *commandWorkflowSpy) CancelRunning(_ context.Context, message string) error {
	workflow.message = message
	return nil
}

func (workflow *actionWorkflowSpy) WaitShutdown(ctx context.Context) error {
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
	}, nil, nil); err != nil {
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
	if runtime.HasActionIdentity() {
		t.Fatal("action identity key was retained")
	}
}

func TestDiscardDoesNotRunActionRecovery(t *testing.T) {
	runtime := &workspaceruntime.Runtime{ID: "opening", ActionIdentityKey: make([]byte, 32)}
	if err := Discard(runtime, nil, nil); err != nil {
		t.Fatal(err)
	}
	if runtime.HasActionIdentity() {
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
	actions := &actionWorkflowSpy{shutdown: func(context.Context) error {
		<-actionRelease
		return nil
	}}
	commands := &commandWorkflowSpy{stop: func(context.Context) error {
		<-commandRelease
		return nil
	}}
	err := closeWithTimeout(runtime,
		func() (ActionWorkflow, error) { return actions, nil },
		func() (CommandWorkflow, error) { return commands, nil }, nil, 10*time.Millisecond)
	if err == nil {
		t.Fatal("worker drain timeout did not defer storage close")
	}
	if !runtime.HasActionIdentity() {
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
	if runtime.HasActionIdentity() {
		t.Fatal("action identity key remained after finalizers drained")
	}
}

func TestCloseRetriesActionResolverBeforeClosingStorage(t *testing.T) {
	storageClosed := make(chan struct{})
	database := sql.OpenDB(closeSignalConnector{closed: storageClosed})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := &workspaceruntime.Runtime{
		ID: "workspace-action-resolver", ActionIdentityKey: make([]byte, 32),
		Storage: workspacestorage.New(database, nil, nil, "workspace-action-resolver", nil),
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	workflow := &actionWorkflowSpy{shutdown: func(context.Context) error {
		close(entered)
		<-release
		return nil
	}}
	var calls atomic.Int32
	err := closeWithTimeout(runtime, func() (ActionWorkflow, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("transient action resolver failure")
		}
		return workflow, nil
	}, nil, nil, 10*time.Millisecond)
	if err == nil {
		t.Fatal("resolver failure did not defer storage close")
	}
	if !runtime.HasActionIdentity() {
		t.Fatal("action identity was cleared before the action owner resolved")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("deferred teardown did not retry the action resolver")
	}
	select {
	case <-storageClosed:
		t.Fatal("storage closed before action workers drained")
	default:
	}
	close(release)
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("storage did not close after the retried action owner drained")
	}
}

func TestCloseRetriesCommandResolverBeforeClosingStorage(t *testing.T) {
	storageClosed := make(chan struct{})
	database := sql.OpenDB(closeSignalConnector{closed: storageClosed})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := &workspaceruntime.Runtime{
		ID: "workspace-command-resolver", ActionIdentityKey: make([]byte, 32),
		Storage: workspacestorage.New(database, nil, nil, "workspace-command-resolver", nil),
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	workflow := &commandWorkflowSpy{stop: func(context.Context) error {
		close(entered)
		<-release
		return nil
	}}
	var calls atomic.Int32
	err := closeWithTimeout(runtime, nil, func() (CommandWorkflow, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("transient command resolver failure")
		}
		return workflow, nil
	}, nil, 10*time.Millisecond)
	if err == nil {
		t.Fatal("resolver failure did not defer storage close")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("deferred teardown did not retry the command resolver")
	}
	select {
	case <-storageClosed:
		t.Fatal("storage closed before command workers drained")
	default:
	}
	close(release)
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("storage did not close after the retried command owner drained")
	}
}

func TestCloseRetriesTransferRecoveryBeforeClosingStorage(t *testing.T) {
	storageClosed := make(chan struct{})
	database := sql.OpenDB(closeSignalConnector{closed: storageClosed})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := &workspaceruntime.Runtime{
		ID:      "workspace-transfer-recovery",
		Storage: workspacestorage.New(database, nil, nil, "workspace-transfer-recovery", nil),
	}
	var recoverCalls atomic.Int32
	transfer := &transferWorkflowSpy{recover: func(context.Context) error {
		if recoverCalls.Add(1) == 1 {
			return errors.New("transient transfer persistence failure")
		}
		return nil
	}}
	if err := closeWithTimeout(runtime, nil, nil, func() TransferWorkflow { return transfer }, 10*time.Millisecond); err == nil {
		t.Fatal("transfer persistence failure did not defer storage close")
	}
	select {
	case <-storageClosed:
		t.Fatal("storage closed before transfer persistence recovered")
	default:
	}
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("storage did not close after transfer persistence retry")
	}
	if recoverCalls.Load() < 2 {
		t.Fatalf("transfer recovery calls = %d", recoverCalls.Load())
	}
}

func TestCloseRetriesTransferBeginBeforeClosingStorage(t *testing.T) {
	storageClosed := make(chan struct{})
	database := sql.OpenDB(closeSignalConnector{closed: storageClosed})
	if err := database.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	runtime := &workspaceruntime.Runtime{
		ID:      "workspace-transfer-begin",
		Storage: workspacestorage.New(database, nil, nil, "workspace-transfer-begin", nil),
	}
	workerRelease := make(chan struct{})
	var beginCalls atomic.Int32
	transfer := &transferWorkflowSpy{
		beginResult: func() (bool, error) {
			if beginCalls.Add(1) == 1 {
				return false, errors.New("transient transfer shutdown failure")
			}
			return true, nil
		},
		wait: func(context.Context) bool {
			<-workerRelease
			return true
		},
	}
	if err := closeWithTimeout(runtime, nil, nil, func() TransferWorkflow { return transfer }, 10*time.Millisecond); err == nil {
		t.Fatal("transfer begin failure did not defer storage close")
	}
	select {
	case <-storageClosed:
		t.Fatal("storage closed after failed transfer shutdown initiation")
	default:
	}
	deadline := time.Now().Add(time.Second)
	for beginCalls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if beginCalls.Load() < 2 {
		t.Fatal("deferred teardown did not retry transfer shutdown initiation")
	}
	select {
	case <-storageClosed:
		t.Fatal("storage closed while transfer workers remained active")
	default:
	}
	close(workerRelease)
	select {
	case <-storageClosed:
	case <-time.After(time.Second):
		t.Fatal("storage did not close after transfer shutdown retry drained workers")
	}
}

type ownershipRetrySpy struct {
	calls        atomic.Int32
	retryRelease <-chan struct{}
}

func (ownership *ownershipRetrySpy) Release() (bool, error) {
	if ownership.calls.Add(1) == 1 {
		return false, errors.New("transient unlock failure")
	}
	if ownership.retryRelease != nil {
		<-ownership.retryRelease
	}
	return true, nil
}

func TestCloseRetriesUnconfirmedOwnershipRelease(t *testing.T) {
	ownership := &ownershipRetrySpy{}
	runtime := &workspaceruntime.Runtime{
		ID:      "workspace-ownership-retry",
		Storage: workspacestorage.New(nil, nil, nil, "workspace-ownership-retry", ownership),
	}
	if err := closeWithTimeout(runtime, nil, nil, nil, 10*time.Millisecond); err == nil {
		t.Fatal("ownership release failure did not defer shutdown")
	}
	if runtime.Storage.DatabaseOwnership() == nil {
		t.Fatal("unconfirmed ownership release was discarded")
	}
	deadline := time.Now().Add(time.Second)
	for runtime.Storage.DatabaseOwnership() != nil && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if runtime.Storage.DatabaseOwnership() != nil || ownership.calls.Load() < 2 {
		t.Fatalf("ownership was not retried: retained=%v calls=%d", runtime.Storage.DatabaseOwnership() != nil, ownership.calls.Load())
	}
}

func TestDiscardRetriesUnconfirmedOwnershipReleaseBeforeCompletion(t *testing.T) {
	retryRelease := make(chan struct{})
	ownership := &ownershipRetrySpy{retryRelease: retryRelease}
	runtime := &workspaceruntime.Runtime{
		ID:      "opening-ownership-retry",
		Storage: workspacestorage.New(nil, nil, nil, "opening-ownership-retry", ownership),
	}
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	err := Discard(runtime, nil, func() {
		close(callbackStarted)
		<-releaseCallback
	})
	if !errors.Is(err, ErrShutdownDeferred) {
		t.Fatalf("Discard() error = %v, want deferred shutdown", err)
	}
	if runtime.Storage.DatabaseOwnership() == nil {
		t.Fatal("discard dropped an unconfirmed ownership release")
	}
	deadline := time.Now().Add(time.Second)
	for ownership.calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if ownership.calls.Load() < 2 {
		t.Fatal("discard retry did not start")
	}
	close(retryRelease)
	select {
	case <-callbackStarted:
	case <-time.After(time.Second):
		t.Fatal("discard completion callback did not start")
	}
	waited := make(chan error, 1)
	go func() { waited <- runtime.WaitTeardown(t.Context()) }()
	select {
	case err := <-waited:
		t.Fatalf("teardown completed before its callback returned: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseCallback)
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("teardown did not complete after its callback returned")
	}
	if runtime.Storage.DatabaseOwnership() != nil || ownership.calls.Load() < 2 {
		t.Fatalf("discard ownership retry retained=%v calls=%d", runtime.Storage.DatabaseOwnership() != nil, ownership.calls.Load())
	}
}

func TestCloseSignalsEveryOwnerBeforeWaiting(t *testing.T) {
	actionWaiting := make(chan struct{})
	actionRelease := make(chan struct{})
	commandBegan := make(chan struct{})
	transferBegan := make(chan struct{})
	var actionWaits atomic.Int32
	actions := &actionWorkflowSpy{shutdown: func(ctx context.Context) error {
		if actionWaits.Add(1) == 1 {
			close(actionWaiting)
			<-actionRelease
		}
		return nil
	}}
	commands := &commandWorkflowSpy{begin: func() { close(commandBegan) }}
	transfer := &transferWorkflowSpy{begin: func() { close(transferBegan) }}
	runtime := &workspaceruntime.Runtime{ID: "workspace-signal-order"}
	if err := closeWithTimeout(runtime,
		func() (ActionWorkflow, error) { return actions, nil },
		func() (CommandWorkflow, error) { return commands, nil },
		func() TransferWorkflow { return transfer }, 10*time.Millisecond); err == nil {
		t.Fatal("blocked action drain unexpectedly completed")
	}
	for name, signal := range map[string]<-chan struct{}{
		"action wait": actionWaiting, "command begin": commandBegan, "transfer begin": transferBegan,
	} {
		select {
		case <-signal:
		default:
			t.Fatalf("%s was not signaled before bounded waiting", name)
		}
	}
	close(actionRelease)
}
