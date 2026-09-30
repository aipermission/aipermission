package runtimeactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

type outcomeConsoleSessions struct {
	connectorapi.ConsoleSessionRuntime
	result connectorapi.ConsoleExecResult
	err    error
}

func (*outcomeConsoleSessions) EnsureReady(context.Context, connectorapi.Principal, int64) (connectorapi.ConsoleSessionHandle, error) {
	return connectorapi.ConsoleSessionHandle{ID: 7, RuntimeID: 11, Generation: 3}, nil
}

func (sessions *outcomeConsoleSessions) Exec(context.Context, connectorapi.Principal, int64, string) (connectorapi.ConsoleExecResult, error) {
	return sessions.result, sessions.err
}

type outcomeActionRuntime struct {
	connectorapi.ActionRuntime
	sessions connectorapi.ConsoleSessionRuntime
}

func (runtime outcomeActionRuntime) ConnectorConsoleSessions() connectorapi.ConsoleSessionRuntime {
	return runtime.sessions
}

func TestSSHUnknownExecutionRetainsExactHandleWithoutOutput(t *testing.T) {
	sessions := &outcomeConsoleSessions{
		result: connectorapi.ConsoleExecResult{SessionID: 7, Generation: 3, Running: true, Command: "withheld-command-fixture", Output: "withheld-output-fixture"},
		err:    fmt.Errorf("observation denied: %w", console.ErrCommandOutcomeUnknown),
	}
	executor := runtimeExecutor{runtime: outcomeActionRuntime{sessions: sessions}}
	result, err := executor.executeCommand(t.Context(), connectorapi.Principal{}, 11, connectors.PreparedAction{Payload: map[string]any{"command": "remote effect"}})
	if err != nil || result.Status != connectors.ResultOutcomeUnknown || result.Handles.SessionID != 7 || result.Handles.SessionGeneration != 3 {
		t.Fatalf("SSH lost dispatched handle/outcome: %#v, %v", result, err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "withheld-") || !strings.Contains(string(encoded), `"retry_safe":false`) || result.DisplayText != "" {
		t.Fatalf("SSH exposed unobserved execution: %s", encoded)
	}
}

func TestSSHConsoleConnectionHonorsParentCancellation(t *testing.T) {
	sessions := &delayedConsoleCommandSessions{readyDelay: time.Second}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := executeConsoleCommand(ctx, sessions, connectorapi.Principal{}, 11, "never dispatch", 5*time.Second, time.Second)
	if !errors.Is(err, context.Canceled) || sessions.execCalled {
		t.Fatalf("parent cancellation was detached: called=%v err=%v", sessions.execCalled, err)
	}
}

func TestSSHBeforeDispatchFailureDoesNotInventHandle(t *testing.T) {
	want := errors.New("connection refused")
	executor := runtimeExecutor{runtime: outcomeActionRuntime{sessions: &outcomeConsoleSessions{err: want}}}
	result, err := executor.executeCommand(t.Context(), connectorapi.Principal{}, 11, connectors.PreparedAction{Payload: map[string]any{"command": "remote effect"}})
	if !errors.Is(err, want) || result.Handles.SessionID != 0 || result.Status == connectors.ResultOutcomeUnknown {
		t.Fatalf("before-dispatch failure misclassified: %#v, %v", result, err)
	}
}

type contextConsoleSessions struct {
	outcomeConsoleSessions
	cancel context.CancelFunc
}

func (sessions *contextConsoleSessions) Exec(ctx context.Context, _ connectorapi.Principal, _ int64, _ string) (connectorapi.ConsoleExecResult, error) {
	sessions.cancel()
	return connectorapi.ConsoleExecResult{}, ctx.Err()
}

func TestSSHExecutionHonorsCancellationAfterConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	sessions := &contextConsoleSessions{cancel: cancel}
	_, err := executeConsoleCommand(ctx, sessions, connectorapi.Principal{}, 11, "never dispatch", time.Second, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("execution detached from parent cancellation: %v", err)
	}
}

func TestSSHConsoleUnknownErrorPreservesTypedClassification(t *testing.T) {
	sessions := &outcomeConsoleSessions{
		result: connectorapi.ConsoleExecResult{SessionID: 7, Generation: 3, Running: true},
		err:    fmt.Errorf("observe: %w", console.ErrCommandOutcomeUnknown),
	}
	result, err := executeConsoleCommand(t.Context(), sessions, connectorapi.Principal{}, 11, "remote effect", time.Second, time.Second)
	if !errors.Is(err, console.ErrCommandOutcomeUnknown) || connectors.ErrorStatus(err) != connectors.ResultOutcomeUnknown || connectors.ErrorCode(err) != "command_outcome_unknown" || result.SessionID != 7 || result.Generation != 3 {
		t.Fatalf("typed classification/identity lost: %#v, %v", result, err)
	}
	if details := connectors.ErrorDetails(err); details["retry_safe"] != false || details["output_withheld"] != true {
		t.Fatalf("unsafe classified error details: %#v", details)
	}
}
