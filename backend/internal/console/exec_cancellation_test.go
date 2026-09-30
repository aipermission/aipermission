package console

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type cancellationWriter struct {
	mu         sync.Mutex
	writes     []string
	first      func()
	afterWrite func(string)
	once       sync.Once
}

func (writer *cancellationWriter) Write(value []byte) (int, error) {
	writer.mu.Lock()
	writer.writes = append(writer.writes, string(value))
	writer.mu.Unlock()
	writer.once.Do(func() {
		if writer.first != nil {
			writer.first()
		}
	})
	if writer.afterWrite != nil {
		writer.afterWrite(string(value))
	}
	return len(value), nil
}

func (*cancellationWriter) Close() error { return nil }

func (writer *cancellationWriter) snapshot() []string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return append([]string(nil), writer.writes...)
}

func newCancellationTestSession(t *testing.T) (*Manager, *managedConsoleSession) {
	t.Helper()
	_, manager, session := newManualHistoryTestSession(t)
	manager.sessions[session.id] = session
	session.ctx = t.Context()
	t.Cleanup(func() {
		session.drainOwnedWork()
		session.flushTranscript()
	})
	return manager, session
}

func TestManagerExecCanceledAdmissionNeverWritesPayload(t *testing.T) {
	for _, phase := range []string{"before-call", "exec-lock", "input-lock"} {
		t.Run(phase, func(t *testing.T) {
			manager, session := newCancellationTestSession(t)
			writer := &cancellationWriter{}
			session.stdin = writer
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var unlock func()
			switch phase {
			case "before-call":
				cancel()
			case "exec-lock":
				session.execMu.Lock()
				unlock = session.execMu.Unlock
			case "input-lock":
				session.inputMu.Lock()
				unlock = session.inputMu.Unlock
			}
			started := make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				close(started)
				result, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, "printf 'never-dispatch-fixture'")
				if result.Running {
					finished <- errors.New("canceled admission reported running")
					return
				}
				finished <- err
			}()
			<-started
			if unlock != nil {
				select {
				case err := <-finished:
					unlock()
					t.Fatalf("request passed held admission lock: %v", err)
				case <-time.After(25 * time.Millisecond):
				}
				cancel()
				unlock()
			}
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled admission error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled request did not return")
			}
			if writes := writer.snapshot(); len(writes) != 0 {
				t.Fatalf("canceled request wrote stdin: %#v", writes)
			}
			if session.activeCommand() != nil {
				t.Fatal("canceled admission retained an active command")
			}
		})
	}
}

func TestConsoleCancellationAfterPreludeRestoresTerminalWithoutPayload(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "prelude-return"
		if blocked {
			name = "blocked-prelude"
		}
		t.Run(name, func(t *testing.T) {
			manager, session := newCancellationTestSession(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered := make(chan struct{})
			release := make(chan struct{})
			writer := &cancellationWriter{first: func() {
				close(entered)
				if blocked {
					<-release
				} else {
					cancel()
				}
			}}
			session.stdin = writer
			finished := make(chan error, 1)
			go func() {
				_, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, "printf 'no-payload-after-prelude'")
				finished <- err
			}()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("prelude did not start")
			}
			if blocked {
				cancel()
				close(release)
			}
			select {
			case err := <-finished:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("prelude cancellation = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("prelude cancellation did not return")
			}
			writes := writer.snapshot()
			if len(writes) != 2 || writes[0] != consoleExecPrelude() || writes[1] != restoreTerminalInputCommand {
				t.Fatalf("unexpected frames: %#v", writes)
			}
			if strings.Contains(strings.Join(writes, ""), "no-payload-after-prelude") || session.activeCommand() != nil {
				t.Fatal("canceled prelude dispatched or retained the user command")
			}
		})
	}
}

func TestConsoleCanceledAuthorizationCallbackNeverDispatches(t *testing.T) {
	_, session := newCancellationTestSession(t)
	writer := &cancellationWriter{}
	session.stdin = writer
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := session.execCommand(ctx, "printf 'never-authorized-dispatch'", func(run func() error) error {
		cancel()
		return run()
	})
	if !errors.Is(err, context.Canceled) || len(writer.snapshot()) != 0 || session.activeCommand() != nil {
		t.Fatalf("canceled callback err=%v writes=%#v", err, writer.snapshot())
	}
}

func TestConsoleCancellationAfterDispatchRetainsReconciliationHandle(t *testing.T) {
	manager, session := newCancellationTestSession(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	writer := &cancellationWriter{afterWrite: func(frame string) {
		if strings.Contains(frame, "after-dispatch-fixture") {
			cancel()
		}
	}}
	session.stdin = writer
	result, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, "printf 'after-dispatch-fixture'")
	if err != nil || !result.Running || result.SessionID != session.id || result.Generation != session.generation {
		t.Fatalf("dispatched cancellation lost reconciliation: %#v, %v", result, err)
	}
	if len(writer.snapshot()) != 2 || session.activeCommand() == nil {
		t.Fatal("dispatched command was cleared or restored before its completion")
	}
}

func TestConsoleCanceledContextStillAllowsCloseCleanup(t *testing.T) {
	manager, session := newCancellationTestSession(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	err := manager.authorizeOperation(ctx, testExecutionPrincipal(), session, OperationClose, func() error {
		called = true
		return nil
	})
	if err != nil || !called {
		t.Fatalf("canceled cleanup was rejected: called=%v err=%v", called, err)
	}
}
