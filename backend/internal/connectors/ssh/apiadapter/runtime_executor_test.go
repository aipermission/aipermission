package apiadapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

type delayedConsoleCommandSessions struct {
	readyDelay time.Duration
	readyErr   error
	execCalled bool
}

func (s *delayedConsoleCommandSessions) EnsureReady(ctx context.Context, _ executionprincipal.Principal, runtimeID int64) (console.SessionHandle, error) {
	select {
	case <-ctx.Done():
		return console.SessionHandle{}, ctx.Err()
	case <-time.After(s.readyDelay):
	}
	if s.readyErr != nil {
		return console.SessionHandle{}, s.readyErr
	}
	return console.SessionHandle{ID: 7, RuntimeID: runtimeID, Generation: 2}, nil
}

func (s *delayedConsoleCommandSessions) Exec(_ context.Context, _ executionprincipal.Principal, runtimeID int64, command string) (console.ExecResult, error) {
	s.execCalled = true
	return console.ExecResult{
		SessionID:  7,
		Generation: 2,
		Command:    command,
		Output:     "ok\n",
		ExitCode:   0,
	}, nil
}

func TestExecuteConsoleCommandUsesSeparateConnectionDeadline(t *testing.T) {
	sessions := &delayedConsoleCommandSessions{readyDelay: 40 * time.Millisecond}
	principal, err := executionprincipal.MCPToken(3, "workspace", "runtime")
	if err != nil {
		t.Fatalf("create principal: %v", err)
	}

	result, err := executeConsoleCommand(sessions, principal, 11, "date -u", 200*time.Millisecond, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("execute command after delayed connection: %v", err)
	}
	if !sessions.execCalled || result.Command != "date -u" || result.SessionID != 7 {
		t.Fatalf("unexpected execution result: %#v, exec_called=%v", result, sessions.execCalled)
	}
}

func TestExecuteConsoleCommandReturnsConnectionError(t *testing.T) {
	sessions := &delayedConsoleCommandSessions{readyErr: errors.New("PTY request rejected")}
	principal, err := executionprincipal.MCPToken(3, "workspace", "runtime")
	if err != nil {
		t.Fatalf("create principal: %v", err)
	}

	_, err = executeConsoleCommand(sessions, principal, 11, "date -u", 200*time.Millisecond, 5*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "start SSH console session: PTY request rejected") {
		t.Fatalf("connection error = %v", err)
	}
	if sessions.execCalled {
		t.Fatal("command executed after connection failure")
	}
}
