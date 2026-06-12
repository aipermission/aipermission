package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/execution"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
)

type connectorRuntimeAdapter interface {
	RuntimeServices(server *Server, runtime *databaseRuntime) map[string]any
	SupportsRunning(prepared actions.PreparedRequest) bool
	FinishRunning(server *Server, runtime *databaseRuntime, requestID int64, prepared actions.PreparedRequest)
	RunningHint(request connectortargets.ActionRequest) string
}

type sshRuntimeAdapter struct{}

func connectorRuntimeAdapterFor(kind string) connectorRuntimeAdapter {
	if kind == sshconnector.Kind {
		return sshRuntimeAdapter{}
	}
	return nil
}

func connectorRuntimeServices(kind string, server *Server, runtime *databaseRuntime) map[string]any {
	adapter := connectorRuntimeAdapterFor(kind)
	if adapter == nil {
		return nil
	}
	return adapter.RuntimeServices(server, runtime)
}

func (sshRuntimeAdapter) RuntimeServices(server *Server, runtime *databaseRuntime) map[string]any {
	return map[string]any{
		sshconnector.RuntimeServiceName: sshConnectorRuntimeExecutor{server: server, runtime: runtime},
	}
}

func (sshRuntimeAdapter) SupportsRunning(prepared actions.PreparedRequest) bool {
	return prepared.Target.ConnectorKind == sshconnector.Kind && prepared.Action.ActionName == sshconnector.ActionExec
}

func (sshRuntimeAdapter) RunningHint(request connectortargets.ActionRequest) string {
	if request.ConnectorKind == sshconnector.Kind && request.ActionName == sshconnector.ActionExec {
		return connectorActionRunningHint
	}
	return ""
}

func (sshRuntimeAdapter) FinishRunning(server *Server, runtime *databaseRuntime, requestID int64, prepared actions.PreparedRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), mcpBackgroundCommandTimeout)
	defer cancel()
	serverID, resolveErr := sshConnectorServerID(context.Background(), runtime, prepared.Action.TargetRef)
	if resolveErr != nil {
		_, _ = server.finishConnectorActionRequest(context.Background(), runtime, requestID, connectors.ResultError, nil, "", resolveErr.Error())
		return
	}
	result, err := runtime.consoleSessions.WaitActive(ctx, serverID)
	status := connectors.ResultStatus("")
	var output any
	var displayText string
	var errorText string
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			_ = runtime.consoleSessions.InterruptActive(context.Background(), serverID)
			status = connectors.ResultError
			errorText = "connector action timed out while running in background"
		} else {
			status = connectors.ResultError
			errorText = err.Error()
		}
	} else {
		status = connectors.ResultCompleted
		if result.ExitCode != 0 {
			status = connectors.ResultFailed
		}
		output = sshExecOutput(result)
		displayText = result.Output
	}
	if status == "" {
		return
	}
	_, _ = server.finishConnectorActionRequest(context.Background(), runtime, requestID, status, output, displayText, errorText)
}

type sshConnectorRuntimeExecutor struct {
	server  *Server
	runtime *databaseRuntime
}

func (e sshConnectorRuntimeExecutor) ExecuteSSHAction(ctx context.Context, _ connectors.RuntimeContext, action connectors.PreparedAction) (connectors.ActionResult, error) {
	if e.server == nil || e.runtime == nil || e.runtime.database == nil {
		return connectors.ActionResult{}, fmt.Errorf("ssh runtime is not available")
	}
	serverID, err := sshConnectorServerID(ctx, e.runtime, action.TargetRef)
	if err != nil {
		return connectors.ActionResult{}, err
	}

	switch action.ActionName {
	case sshconnector.ActionExec:
		return e.executeCommand(serverID, action)
	case sshconnector.ActionReadConsole:
		return e.readConsole(ctx, serverID, action)
	case sshconnector.ActionRestartConsoleSession:
		return e.restartConsole(ctx, serverID)
	case sshconnector.ActionBrowseRemoteFiles:
		return e.browseRemoteFiles(ctx, serverID, action)
	case sshconnector.ActionStartFileDownload:
		return e.startFileDownload(ctx, serverID, action)
	default:
		return connectors.ActionResult{}, fmt.Errorf("%w: %s", sshconnector.ErrUnsupportedAction, action.ActionName)
	}
}

func (e sshConnectorRuntimeExecutor) executeCommand(serverID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	command := stringPayload(action.Payload, "command")
	if command == "" {
		return connectors.ActionResult{}, fmt.Errorf("command is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), mcpInitialExecTimeout)
	defer cancel()
	result, err := e.runtime.consoleSessions.Exec(ctx, serverID, command)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	output := sshExecOutput(result)
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
			"server_id":   serverID,
			"duration_ms": result.DurationMS,
		},
		Handles: connectors.ActionHandles{
			SessionID: result.SessionID,
		},
	}
	if result.Running {
		response.Error = "SSH command is still running in the persistent console session."
		response.Handles.FollowupTool = "get_connector_action_request"
	}
	return response, nil
}

func (e sshConnectorRuntimeExecutor) readConsole(ctx context.Context, serverID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	tail := intPayload(action.Payload, "tail_bytes", 20000)
	if tail < 1 {
		tail = 20000
	}
	if tail > 100000 {
		tail = 100000
	}
	sessions, err := e.runtime.consoleSessions.List(ctx, serverID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if len(sessions) == 0 {
		return connectors.ActionResult{
			Status: connectors.ResultCompleted,
			Output: map[string]any{
				"server_id": serverID,
				"status":    "none",
			},
		}, nil
	}
	session := sessions[0]
	transcript := console.PlainOutput(console.TailStringByBytes(session.Transcript, tail))
	return connectors.ActionResult{
		Status:      connectors.ResultCompleted,
		DisplayText: transcript,
		Output: map[string]any{
			"server_id":  serverID,
			"session_id": session.ID,
			"status":     session.Status,
			"transcript": transcript,
			"error":      session.Error,
			"tail_bytes": tail,
		},
		Handles: connectors.ActionHandles{SessionID: session.ID},
	}, nil
}

func (e sshConnectorRuntimeExecutor) restartConsole(ctx context.Context, serverID int64) (connectors.ActionResult, error) {
	result, err := e.server.restartServerConsoleSession(ctx, e.runtime, serverID, "console session restarted before connector action completed")
	if err != nil {
		return connectors.ActionResult{}, err
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"server_id":                 serverID,
			"closed_session_ids":        result.ClosedSessionIDs,
			"canceled_running_requests": result.CanceledRunningRequests,
		},
		DisplayText: "SSH console session restarted.",
	}, nil
}

func (e sshConnectorRuntimeExecutor) browseRemoteFiles(ctx context.Context, serverID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	remotePath, err := normalizeRemoteDirectoryPath(stringPayload(action.Payload, "path"))
	if err != nil {
		return connectors.ActionResult{}, err
	}
	if remotePath == "" {
		remotePath = "~"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	server, privateKey, err := fileTransferHandlers{e.server}.serverSSHMaterialFromRuntime(ctx, e.runtime, serverID)
	if err != nil {
		return connectors.ActionResult{}, err
	}
	entries, err := execution.ListRemoteDirectory(ctx, e.server.executionTarget(server, privateKey), remotePath)
	if err != nil {
		return connectors.ActionResult{}, fmt.Errorf("%s", sshConnectionFailureMessage(err))
	}
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"server_id": serverID,
			"path":      remotePath,
			"parent":    sshBrowseParent(remotePath),
			"entries":   entries,
		},
	}, nil
}

func (e sshConnectorRuntimeExecutor) startFileDownload(ctx context.Context, serverID int64, action connectors.PreparedAction) (connectors.ActionResult, error) {
	remotePaths := stringSlicePayload(action.Payload, "remote_paths")
	if len(remotePaths) == 0 {
		return connectors.ActionResult{}, fmt.Errorf("remote_paths is required")
	}
	archiveName := stringPayload(action.Payload, "archive_name")
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	transfers := fileTransferHandlers{e.server}
	batch, err := transfers.createDownloadBatch(ctx, e.runtime, serverID, remotePaths, archiveName, filetransfer.SourceMCP, filetransfer.StatusPending)
	if err != nil {
		if startErr, ok := err.(*fileTransferStartError); ok && startErr.Status == http.StatusBadRequest {
			return connectors.ActionResult{}, fmt.Errorf("%s", startErr.Message)
		}
		return connectors.ActionResult{}, err
	}
	go transfers.runTransferBatch(e.runtime, batch.ID, false)
	return connectors.ActionResult{
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"server_id": serverID,
			"batch_id":  batch.ID,
			"status":    batch.Status,
			"items":     len(batch.Items),
		},
		DisplayText: "SSH download queue started.",
		Handles: connectors.ActionHandles{
			BatchID: batch.ID,
		},
	}, nil
}

func sshConnectorServerID(ctx context.Context, runtime *databaseRuntime, targetRef string) (int64, error) {
	_, profile, err := connectortargets.NewStore(runtime.database).SSHRuntimeForConsoleRef(ctx, targetRef)
	if err != nil {
		return 0, err
	}
	if profile.ID < 1 {
		return 0, fmt.Errorf("ssh connector target has no runtime profile")
	}
	return profile.ID, nil
}

func sshExecOutput(result console.ExecResult) map[string]any {
	return map[string]any{
		"command":     result.Command,
		"stdout":      console.PlainOutput(result.Output),
		"stderr":      "",
		"exit_code":   result.ExitCode,
		"running":     result.Running,
		"session_id":  result.SessionID,
		"duration_ms": result.DurationMS,
	}
}

func stringPayload(payload map[string]any, name string) string {
	value, ok := payload[name]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func intPayload(payload map[string]any, name string, fallback int) int {
	value, ok := payload[name]
	if !ok || value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return fallback
	}
}

func stringSlicePayload(payload map[string]any, name string) []string {
	value, ok := payload[name]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, fmt.Sprint(item))
		}
		return out
	case string:
		if typed == "" {
			return nil
		}
		return []string{typed}
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func sshBrowseParent(remotePath string) string {
	if remotePath == "" || remotePath == "/" || remotePath == "." {
		return "/"
	}
	parent := path.Dir(remotePath)
	if parent == "." {
		return "/"
	}
	return parent
}
