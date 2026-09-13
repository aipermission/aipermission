package commandrequests

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type testActiveSessions struct {
	result     console.ExecResult
	err        error
	interrupts int
	wait       func(context.Context) (console.ExecResult, error)
}

type transientCommandProjection struct {
	failures int
	ids      []int64
}

func (projection *transientCommandProjection) SyncCommandRequest(_ context.Context, _ Executor, id int64) error {
	if projection.failures > 0 {
		projection.failures--
		return errors.New("temporary projection failure")
	}
	projection.ids = append(projection.ids, id)
	return nil
}

func TestRuntimeRecoversUnpersistedTerminalResultAsOutcomeUnknown(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	id, err := NewStore(database).Insert(t.Context(), testCommandCodec{}, &testCommandProjection{}, PreparedInsert{
		insert: Insert{RuntimeID: runtimeID, Command: "remote mutation", Status: "running"}, storedCommand: "remote mutation",
	})
	if err != nil {
		t.Fatal(err)
	}
	projection := &transientCommandProjection{failures: commandPersistenceAttempts * 2}
	owner := newTestRuntime(t, database, &testActiveSessions{
		result: console.ExecResult{SessionID: 44, ExitCode: 0, Output: "committed"},
	}, projection)
	principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.RunWorker(func(ctx context.Context) {
		owner.FinishActive(ctx, id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 1})
	}) {
		t.Fatal("background command worker was not admitted")
	}
	if err := owner.StopWorkers(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := owner.CancelRunning(t.Context(), "workspace closed"); err != nil {
		t.Fatal(err)
	}
	stored, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "outcome_unknown" || stored.Error != commandOutcomeUnknown {
		t.Fatalf("uncertain remote result = %#v", stored)
	}
}

func (s *testActiveSessions) WaitActive(
	ctx context.Context,
	_ executionprincipal.Principal,
	_ console.SessionHandle,
) (console.ExecResult, error) {
	if s.wait != nil {
		return s.wait(ctx)
	}
	return s.result, s.err
}

func (s *testActiveSessions) InterruptActive(
	context.Context,
	executionprincipal.Principal,
	console.SessionHandle,
) error {
	s.interrupts++
	return nil
}

func TestRuntimeRedactsTerminalResultsBeforePersistence(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	projection := &testCommandProjection{}
	owner := newTestRuntime(t, database, &testActiveSessions{}, projection)
	id, err := owner.Insert(t.Context(), Insert{
		RuntimeID: runtimeID, Command: "echo secret", Reason: "secret reason", Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Finish(t.Context(), Completion{
		ID: id, Status: "error", Stdout: "\x1b[31msecret output", Stderr: "secret stderr",
		ExitCode: 1, Error: "secret failure",
	}); err != nil {
		t.Fatal(err)
	}
	item, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Command != "echo [REDACTED]" || item.Reason != "[REDACTED] reason" ||
		item.Stdout != "[REDACTED] output" || item.Stderr != "[REDACTED] stderr" ||
		item.Error != "[REDACTED] failure" {
		t.Fatalf("redacted command request = %#v", item)
	}
	command, err := owner.ExecutionCommand(t.Context(), id)
	if err != nil || command != "echo secret" {
		t.Fatalf("ExecutionCommand() = %q, %v", command, err)
	}
}

func TestRuntimeFinishesBackgroundCommandsAndInterruptsTimeouts(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	projection := &testCommandProjection{}
	sessions := &testActiveSessions{result: console.ExecResult{SessionID: 44, ExitCode: 0, Output: "done"}}
	owner := newTestRuntime(t, database, sessions, projection)
	id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "sleep", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	owner.FinishActive(t.Context(), id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 1})
	item, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil || item.Status != "completed" || item.Stdout != "done" {
		t.Fatalf("completed background request = %#v, %v", item, err)
	}

	sessions.err = context.DeadlineExceeded
	timedOutID, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "sleep 60", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	owner.FinishActive(t.Context(), timedOutID, principal, console.SessionHandle{ID: 45, RuntimeID: runtimeID, Generation: 1})
	timedOut, err := NewStore(database).Get(t.Context(), timedOutID, 0, "")
	if err != nil || timedOut.Status != "error" || !strings.Contains(timedOut.Error, "timed out") || sessions.interrupts != 1 {
		t.Fatalf("timed-out background request = %#v interrupts=%d err=%v", timedOut, sessions.interrupts, err)
	}
}

func TestRuntimeRetriesTerminalPersistenceBeforeWorkerDrain(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	id, err := NewStore(database).Insert(t.Context(), testCommandCodec{}, &testCommandProjection{}, PreparedInsert{
		insert: Insert{RuntimeID: runtimeID, Command: "echo done", Status: "running"}, storedCommand: "echo done",
	})
	if err != nil {
		t.Fatal(err)
	}
	projection := &transientCommandProjection{failures: 1}
	owner := newTestRuntime(t, database, &testActiveSessions{
		result: console.ExecResult{SessionID: 44, ExitCode: 0, Output: "done"},
	}, projection)
	principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.RunWorker(func(ctx context.Context) {
		owner.FinishActive(ctx, id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 1})
	}) {
		t.Fatal("background command worker was not admitted")
	}
	if err := owner.WaitWorkers(t.Context()); err != nil {
		t.Fatalf("worker finalization error = %v", err)
	}
	stored, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil || stored.Status != "completed" || stored.Stdout != "done" {
		t.Fatalf("retried completion = %#v, %v", stored, err)
	}
	if len(projection.ids) != 1 || projection.ids[0] != id {
		t.Fatalf("terminal projection ids = %v", projection.ids)
	}
}

func TestRuntimeRejectsIncompleteDependencies(t *testing.T) {
	if _, err := NewRuntime(RuntimeDependencies{}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	var owner *Runtime
	if _, err := owner.Prepare(t.Context(), Insert{}); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("nil Runtime.Prepare() error = %v", err)
	}
	if err := owner.CancelRunning(t.Context(), "closed"); !errors.Is(err, ErrRuntimeUnavailable) {
		t.Fatalf("nil Runtime.CancelRunning() error = %v", err)
	}
}

func TestRuntimeStopWorkersCancelsDrainsAndClosesAdmission(t *testing.T) {
	database, _ := commandRequestFixture(t)
	owner := newTestRuntime(t, database, &testActiveSessions{}, &testCommandProjection{})
	started := make(chan struct{})
	finished := make(chan struct{})
	if !owner.RunWorker(func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	}) {
		t.Fatal("worker was not admitted before shutdown")
	}
	<-started
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := owner.StopWorkers(ctx); err != nil {
		t.Fatalf("StopWorkers() error = %v", err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("StopWorkers returned before the active worker exited")
	}
	if owner.RunWorker(func(context.Context) {}) {
		t.Fatal("worker was admitted after shutdown")
	}
}

func TestRuntimeShutdownLeavesActiveCommandForCanonicalRecovery(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	started := make(chan struct{})
	sessions := &testActiveSessions{wait: func(ctx context.Context) (console.ExecResult, error) {
		close(started)
		<-ctx.Done()
		return console.ExecResult{}, ctx.Err()
	}}
	owner := newTestRuntime(t, database, sessions, &testCommandProjection{})
	id, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "sleep 60", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := executionprincipal.LocalOperator("workspace", "runtime-instance")
	if err != nil {
		t.Fatal(err)
	}
	if !owner.RunWorker(func(ctx context.Context) {
		owner.FinishActive(ctx, id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 1})
	}) {
		t.Fatal("background command worker was not admitted")
	}
	<-started
	shutdownCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := owner.StopWorkers(shutdownCtx); err != nil {
		t.Fatalf("StopWorkers() error = %v", err)
	}
	item, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if item.Status != "running" || item.Error != "" {
		t.Fatalf("shutdown worker persisted a terminal result before recovery: %#v", item)
	}
	if sessions.interrupts != 0 {
		t.Fatalf("shutdown worker interrupted an already-closing session %d times", sessions.interrupts)
	}
	if err := owner.CancelRunning(t.Context(), "workspace locked while command was running"); err != nil {
		t.Fatal(err)
	}
	recovered, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "error" || recovered.Error != "workspace locked while command was running" {
		t.Fatalf("canonical recovery result = %#v", recovered)
	}
}

func TestRuntimeStopWorkersReportsBoundedWaitAndCanObserveLaterDrain(t *testing.T) {
	database, _ := commandRequestFixture(t)
	owner := newTestRuntime(t, database, &testActiveSessions{}, &testCommandProjection{})
	release := make(chan struct{})
	if !owner.RunWorker(func(context.Context) { <-release }) {
		t.Fatal("worker was not admitted")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := owner.StopWorkers(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopWorkers() error = %v, want deadline exceeded", err)
	}
	firstDrain := owner.workerDone
	secondCtx, secondCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer secondCancel()
	if err := owner.StopWorkers(secondCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second bounded StopWorkers() error = %v, want deadline exceeded", err)
	}
	if owner.workerDone != firstDrain {
		t.Fatal("StopWorkers replaced the reusable drain signal")
	}
	close(release)
	drainCtx, drainCancel := context.WithTimeout(t.Context(), time.Second)
	defer drainCancel()
	if err := owner.StopWorkers(drainCtx); err != nil {
		t.Fatalf("second StopWorkers() did not observe eventual drain: %v", err)
	}
}

func newTestRuntime(
	t *testing.T,
	database *sql.DB,
	sessions ActiveSessions,
	projection Projection,
) *Runtime {
	t.Helper()
	owner, err := NewRuntime(RuntimeDependencies{
		Store: NewStore(database), Codec: testCommandCodec{}, Projection: projection,
		Redact: func(_ context.Context, value string) string {
			return strings.ReplaceAll(value, "secret", "[REDACTED]")
		},
		Sessions: sessions, BackgroundTimeout: time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	return owner
}
