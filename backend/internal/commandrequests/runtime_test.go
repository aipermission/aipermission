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
}

func (s *testActiveSessions) WaitActive(
	context.Context,
	executionprincipal.Principal,
	console.SessionHandle,
) (console.ExecResult, error) {
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
	owner.FinishActive(id, principal, console.SessionHandle{ID: 44, RuntimeID: runtimeID, Generation: 1})
	item, err := NewStore(database).Get(t.Context(), id, 0, "")
	if err != nil || item.Status != "completed" || item.Stdout != "done" {
		t.Fatalf("completed background request = %#v, %v", item, err)
	}

	sessions.err = context.DeadlineExceeded
	timedOutID, err := owner.Insert(t.Context(), Insert{RuntimeID: runtimeID, Command: "sleep 60", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	owner.FinishActive(timedOutID, principal, console.SessionHandle{ID: 45, RuntimeID: runtimeID, Generation: 1})
	timedOut, err := NewStore(database).Get(t.Context(), timedOutID, 0, "")
	if err != nil || timedOut.Status != "error" || !strings.Contains(timedOut.Error, "timed out") || sessions.interrupts != 1 {
		t.Fatalf("timed-out background request = %#v interrupts=%d err=%v", timedOut, sessions.interrupts, err)
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
