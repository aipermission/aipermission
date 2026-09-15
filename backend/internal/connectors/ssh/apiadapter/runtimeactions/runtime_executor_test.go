package runtimeactions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

func TestIntPayloadAcceptsSealedJSONNumbers(t *testing.T) {
	if got := intPayload(map[string]any{"tail_bytes": json.Number("2048")}, "tail_bytes", 10); got != 2048 {
		t.Fatalf("tail_bytes = %d", got)
	}
}

func TestRuntimeCapabilityForActionSeparatesConsoleAndFileTransfers(t *testing.T) {
	for _, testCase := range []struct {
		action string
		want   string
	}{
		{action: sshconnector.ActionExec, want: connectortargets.RuntimeCapabilityLiveConsole},
		{action: sshconnector.ActionReadConsole, want: connectortargets.RuntimeCapabilityLiveConsole},
		{action: sshconnector.ActionRestartConsoleSession, want: connectortargets.RuntimeCapabilityLiveConsole},
		{action: sshconnector.ActionBrowseRemoteFiles, want: connectortargets.RuntimeCapabilityFileTransfer},
		{action: sshconnector.ActionStartFileDownload, want: connectortargets.RuntimeCapabilityFileTransfer},
	} {
		if got := runtimeCapabilityForAction(testCase.action); got != testCase.want {
			t.Fatalf("capability for %s = %q, want %q", testCase.action, got, testCase.want)
		}
	}
}

func TestReadConsoleReturnsExactSessionHandle(t *testing.T) {
	handles := exactSessionActionHandles(connectorapi.ConsoleRecord{ID: 12, Generation: 34})
	if handles.SessionID != 12 || handles.SessionGeneration != 34 {
		t.Fatalf("read console handle = %#v", handles)
	}
}

type delayedConsoleCommandSessions struct {
	readyDelay time.Duration
	readyErr   error
	execCalled bool
}

func (s *delayedConsoleCommandSessions) EnsureReady(ctx context.Context, _ connectorapi.Principal, runtimeID int64) (connectorapi.ConsoleSessionHandle, error) {
	select {
	case <-ctx.Done():
		return connectorapi.ConsoleSessionHandle{}, ctx.Err()
	case <-time.After(s.readyDelay):
	}
	if s.readyErr != nil {
		return connectorapi.ConsoleSessionHandle{}, s.readyErr
	}
	return connectorapi.ConsoleSessionHandle{ID: 7, RuntimeID: runtimeID, Generation: 2}, nil
}

func (s *delayedConsoleCommandSessions) Exec(_ context.Context, _ connectorapi.Principal, runtimeID int64, command string) (connectorapi.ConsoleExecResult, error) {
	s.execCalled = true
	return connectorapi.ConsoleExecResult{
		SessionID:  7,
		Generation: 2,
		Command:    command,
		Output:     "ok\n",
		ExitCode:   0,
	}, nil
}

func TestExecuteConsoleCommandUsesSeparateConnectionDeadline(t *testing.T) {
	sessions := &delayedConsoleCommandSessions{readyDelay: 40 * time.Millisecond}
	principal := connectorapi.Principal{Kind: connectorapi.PrincipalMCPToken, TokenID: 3, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}

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
	principal := connectorapi.Principal{Kind: connectorapi.PrincipalMCPToken, TokenID: 3, WorkspaceID: "workspace", RuntimeInstanceID: "runtime"}

	_, err := executeConsoleCommand(sessions, principal, 11, "date -u", 200*time.Millisecond, 5*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "start SSH console session: PTY request rejected") {
		t.Fatalf("connection error = %v", err)
	}
	if sessions.execCalled {
		t.Fatal("command executed after connection failure")
	}
}
