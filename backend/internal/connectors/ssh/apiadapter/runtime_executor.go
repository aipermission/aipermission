package apiadapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/execution"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sessionenvprotocol"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type runtimeExecutor struct {
	server  connectorapi.GatewayServer
	runtime connectorapi.GatewayRuntime
}

type sessionEnvironmentCapability struct{}

type consoleCommandSessions interface {
	EnsureReady(context.Context, executionprincipal.Principal, int64) (console.SessionHandle, error)
	Exec(context.Context, executionprincipal.Principal, int64, string) (console.ExecResult, error)
}

func (sessionEnvironmentCapability) ConnectorRuntimeCapability() string {
	return connectors.SessionEnvironmentCapabilityName
}

func (sessionEnvironmentCapability) SessionEnvironmentVersion() string {
	return sessionenvprotocol.Version
}

func (sessionEnvironmentCapability) SessionEnvironmentPeerIdentityRequired() bool {
	return true
}

func (runtimeExecutor) ConnectorRuntimeCapability() string {
	return sshconnector.RuntimeServiceName
}

func (e runtimeExecutor) ExecuteSSHAction(ctx context.Context, runtimeContext connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if e.server == nil || e.runtime == nil {
		return connectors.ActionResult{}, fmt.Errorf("ssh runtime is not available")
	}
	runtimeID, err := runtimeIDForTargetRef(ctx, e.runtime, action.TargetRef)
	if err != nil {
		return connectors.ActionResult{}, err
	}

	switch action.ActionName {
	case sshconnector.ActionExec:
		return e.executeCommand(runtimeContext.Principal, runtimeID, action)
	case sshconnector.ActionReadConsole:
		return e.readConsole(ctx, runtimeContext.Principal, runtimeID, action)
	case sshconnector.ActionRestartConsoleSession:
		return e.restartConsole(ctx, runtimeContext.Principal, runtimeID)
	case sshconnector.ActionBrowseRemoteFiles:
		return e.browseRemoteFiles(ctx, runtimeID, action)
	case sshconnector.ActionStartFileDownload:
		return e.startFileDownload(ctx, runtimeID, action)
	default:
		return connectors.ActionResult{}, fmt.Errorf("%w: %s", sshconnector.ErrUnsupportedAction, action.ActionName)
	}
}

func (e runtimeExecutor) executeCommand(principal executionprincipal.Principal, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	command := stringPayload(action.Payload, "command")
	if command == "" {
		return connectors.ActionResult{}, fmt.Errorf("command is required")
	}
	sessions, err := consoleSessions(e.runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result, err := executeConsoleCommand(sessions, principal, runtimeID, command, consoleConnectTimeout, initialExecTimeout)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	output := execOutput(result)
	status := connectors.ResultCompleted
	if result.Running {
		status = connectors.ResultRunning
	} else if result.ExitCode != 0 {
		status = connectors.ResultFailed
	}
	response := connectors.ActionResult{
		Status:      status,
		Output:      output,
		DisplayText: output["stdout"].(string),
		Metadata: map[string]any{
			"runtime_id":  runtimeID,
			"duration_ms": result.DurationMS,
		},
		Handles: connectors.ActionHandles{
			SessionID:         result.SessionID,
			SessionGeneration: result.Generation,
		},
	}
	if result.Running {
		response.Error = "SSH command is still running in the persistent console session."
		response.Handles.FollowupTool = "get_connector_action_request"
	}
	return response, nil
}

func executeConsoleCommand(sessions consoleCommandSessions, principal executionprincipal.Principal, runtimeID int64, command string, connectTimeout time.Duration, commandTimeout time.Duration) (console.ExecResult, error) {
	connectCtx, cancelConnect := context.WithTimeout(context.Background(), connectTimeout)
	_, err := sessions.EnsureReady(connectCtx, principal, runtimeID)
	cancelConnect()
	if err != nil {
		return console.ExecResult{}, fmt.Errorf("start SSH console session: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	result, err := sessions.Exec(ctx, principal, runtimeID, command)
	if err != nil {
		if errors.Is(err, console.ErrCommandOutcomeUnknown) {
			return result, connectors.ClassifyActionError(
				"command_outcome_unknown",
				connectors.ResultOutcomeUnknown,
				map[string]any{
					"command_dispatched": true,
					"retry_safe":         false,
					"output_withheld":    true,
				},
				err,
			)
		}
		return console.ExecResult{}, err
	}
	return result, nil
}

func (e runtimeExecutor) readConsole(ctx context.Context, principal executionprincipal.Principal, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	tail := intPayload(action.Payload, "tail_bytes", 20000)
	if tail < 1 {
		tail = 20000
	}
	if tail > 100000 {
		tail = 100000
	}
	sessions, err := consoleSessions(e.runtime)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	session, err := sessions.ActiveSnapshot(ctx, principal, runtimeID)
	if errors.Is(err, console.ErrNotFound) {
		return connectors.ActionResult{
			Status: connectors.ResultCompleted,
			Output: map[string]any{
				"runtime_id": runtimeID,
				"status":     "none",
			},
		}, nil
	}
	if err != nil {
		return connectors.ActionResult{}, err
	}
	transcript := console.PlainOutput(console.TailStringByBytes(session.Transcript, tail))
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		DisplayText: transcript,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"session_id": session.ID,
			"status":     session.Status,
			"transcript": transcript,
			"error":      session.Error,
			"tail_bytes": tail,
		},
		Handles: exactSessionActionHandles(session),
	}, nil
}

func exactSessionActionHandles(session console.Record) connectors.ActionHandles {
	return connectors.ActionHandles{
		SessionID:         session.ID,
		SessionGeneration: session.Generation,
	}
}

func (e runtimeExecutor) restartConsole(ctx context.Context, principal executionprincipal.Principal, runtimeID int64) (connectors.ActionResult, error) {
	gateway, err := serverFrom(e.server)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	result, err := gateway.ConnectorRestartConsoleSession(ctx, e.runtime, principal, runtimeID, "console session restarted before connector action completed")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id":                runtimeID,
			"closed_session_ids":        result.ClosedSessionIDs,
			"canceled_running_requests": result.CanceledRunningRequests,
		},
		DisplayText: "SSH console session restarted.",
	}, nil
}

func (e runtimeExecutor) browseRemoteFiles(ctx context.Context, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	remotePath, err := normalizeRemoteDirectoryPath(stringPayload(action.Payload, "path"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if remotePath == "" {
		remotePath = "~"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	gateway, err := serverFrom(e.server)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	target, privateKey, err := targetMaterial(ctx, e.runtime, runtimeID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, executionTarget(gateway, target, privateKey), remotePath)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("%s", connectionFailureMessage(err))
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"path":       remotePath,
			"parent":     browseParent(remotePath),
			"entries":    entries,
		},
	}, nil
}

func (e runtimeExecutor) startFileDownload(ctx context.Context, runtimeID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	remotePaths := stringSlicePayload(action.Payload, "remote_paths")
	if len(remotePaths) == 0 {
		return connectors.ActionResult{}, fmt.Errorf("remote_paths is required")
	}
	archiveName := stringPayload(action.Payload, "archive_name")
	gateway, err := serverFrom(e.server)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	batch, err := gateway.ConnectorCreateDownloadBatch(ctx, e.runtime, runtimeID, remotePaths, archiveName, filetransfer.SourceMCP, filetransfer.StatusPending)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	go gateway.ConnectorRunTransferBatch(e.runtime, batch.ID, false)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"runtime_id": runtimeID,
			"batch_id":   batch.ID,
			"status":     batch.Status,
			"items":      len(batch.Items),
		},
		DisplayText: "SSH download queue started.",
		Handles: connectors.ActionHandles{
			BatchID: batch.ID,
		},
	}, nil
}
