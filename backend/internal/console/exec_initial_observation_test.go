package console

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/sessionenv"
)

func TestInitialCommandObservationLossRetainsDispatchedIdentity(t *testing.T) {
	for _, phase := range []string{"closed", "error", "invalid-marker"} {
		t.Run(phase, func(t *testing.T) {
			manager, session := newCancellationTestSession(t)
			command := "printf initial-observation-fixture"
			writer := &cancellationWriter{afterWrite: func(frame string) {
				if !strings.Contains(frame, command) {
					return
				}
				if phase == "invalid-marker" {
					active := session.activeCommand()
					session.mu.Lock()
					session.rawTranscript = "private-output\n" + active.Marker + ":invalid\n"
					session.mu.Unlock()
				} else {
					session.setStatus(phase, "transport stopped before completion")
				}
			}}
			session.stdin = writer
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, command)
			assertUnknownInitialExecution(t, session, result, err)
			if frames := writer.snapshot(); len(frames) != 2 || !strings.Contains(frames[1], command) {
				t.Fatalf("payload frames = %#v", frames)
			}
			if phase == "invalid-marker" {
				other, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, "printf never-retry-implicitly")
				if !errors.Is(err, ErrCommandActive) || errors.Is(err, ErrCommandOutcomeUnknown) || other.SessionID != 0 ||
					other.Command != "" || other.Running || len(writer.snapshot()) != 2 {
					t.Fatalf("blocked command borrowed previous dispatch identity: %#v %v", other, err)
				}
			}
		})
	}
}

func TestInitialCommandValidMarkerWinsOverTransportClose(t *testing.T) {
	manager, session := newCancellationTestSession(t)
	command := "printf observed-exit-fixture"
	writer := &cancellationWriter{afterWrite: func(frame string) {
		if !strings.Contains(frame, command) {
			return
		}
		active := session.activeCommand()
		session.mu.Lock()
		session.rawTranscript = "observed-output\n" + active.Marker + ":7\n"
		session.mu.Unlock()
		session.setStatus("closed", "transport stopped after marker")
	}}
	session.stdin = writer
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	result, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, command)
	if err != nil || result.Running || result.ExitCode != 7 || result.Output != "observed-output" || result.SessionID != session.id {
		t.Fatalf("valid marker lost: %#v %v", result, err)
	}
}

type uncertainPayloadWriter struct {
	*cancellationWriter
	mode string
}

func (writer uncertainPayloadWriter) Write(value []byte) (int, error) {
	if !strings.Contains(string(value), "uncertain-write-fixture") {
		return writer.cancellationWriter.Write(value)
	}
	_, _ = writer.cancellationWriter.Write(value)
	switch writer.mode {
	case "short":
		return len(value) / 2, nil
	case "partial-error":
		return len(value) / 2, fmt.Errorf("payload transport failed")
	default:
		return 0, fmt.Errorf("payload acknowledgement lost")
	}
}

func TestInitialCommandPayloadWriteUncertaintyNeverReportsSafeFailure(t *testing.T) {
	for _, mode := range []string{"short", "partial-error", "unacknowledged"} {
		t.Run(mode, func(t *testing.T) {
			manager, session := newCancellationTestSession(t)
			writer := uncertainPayloadWriter{cancellationWriter: &cancellationWriter{}, mode: mode}
			session.stdin = writer
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			result, err := manager.Exec(ctx, testExecutionPrincipal(), session.runtimeID, "printf uncertain-write-fixture")
			assertUnknownInitialExecution(t, session, result, err)
			if len(writer.snapshot()) != 2 {
				t.Fatal("payload write failed before its attempt")
			}
		})
	}
}

func assertUnknownInitialExecution(t *testing.T, session *managedConsoleSession, result ExecResult, err error) {
	t.Helper()
	if !errors.Is(err, ErrCommandOutcomeUnknown) || result.SessionID != session.id || result.Generation != session.generation ||
		!result.Running || result.Output != "" || result.Command == "" || session.activeCommand() == nil {
		t.Fatalf("dispatch uncertainty lost: %#v %v", result, err)
	}
}

func TestInitialCommandReadinessRejectionDoesNotInventDispatch(t *testing.T) {
	_, session := newCancellationTestSession(t)
	writer := &cancellationWriter{first: func() { session.setStatus("closed", "prelude transport stopped") }}
	session.stdin = writer
	result, err := session.execCommand(t.Context(), "printf never-dispatched", nil)
	if err == nil || errors.Is(err, ErrCommandOutcomeUnknown) || result.Running || result.SessionID != 0 ||
		session.activeCommand() != nil || len(writer.snapshot()) != 1 {
		t.Fatalf("readiness rejection invented dispatch: %#v %v", result, err)
	}
}

func TestInitialCommandAuthorizationFailureAfterWriteRetainsSafeIdentity(t *testing.T) {
	_, session := newCancellationTestSession(t)
	const secret = "canary-canary-canary"
	environment, err := sessionenv.NewEnvelope([]sessionenv.EntryInput{{Name: "FIXTURE_VALUE", Value: []byte(secret)}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(environment.Destroy)
	session.environment = environment
	writer := &cancellationWriter{}
	session.stdin = writer
	cause := fmt.Errorf("authorization changed %s", secret)
	result, err := session.execCommand(t.Context(), "printf safe-command-fixture", func(run func() error) error {
		if err := run(); err != nil {
			return err
		}
		return cause
	})
	assertUnknownInitialExecution(t, session, result, err)
	if !errors.Is(err, cause) || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "[REDACTED VAULT VALUE]") {
		t.Fatalf("unsafe or unclassified observation: %v", err)
	}
	session.closeExactRedactor()
	if strings.Contains(err.Error(), secret) || len(writer.snapshot()) != 2 {
		t.Fatal("late error projection lost redaction or dispatch identity")
	}
}
