package running

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/management"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/apiadapter/runtimeactions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
)

const (
	backgroundCommandTimeout = 30 * time.Minute
	finishRequestTimeout     = 10 * time.Second
)

type Running struct{}

func (Running) SupportsRunning(prepared connectors.RuntimeActionContext) bool {
	return prepared.TargetConnectorKind == sshconnector.Kind && prepared.ActionName == sshconnector.ActionExec
}

func (Running) RunningHint(request connectorapi.ActionRequest) string {
	if request.ConnectorKind == sshconnector.Kind && request.ActionName == sshconnector.ActionExec {
		return "Wait 3 seconds, then call get_connector_action_request again. For SSH exec actions, inspect live output with the read_console connector action before sending another long-running command to the same target. If the action appears stuck, use the restart_console_session connector action for that target."
	}
	return ""
}

func (Running) FinishRunning(parent context.Context, server connectorapi.ActionFinishGateway, runtime connectorapi.ActionRuntime, requestID int64, prepared connectors.RuntimeActionContext, principal connectorapi.Principal, handles connectors.ActionHandles) error {
	if server == nil {
		return errors.New("finish running connector action: gateway server is unavailable")
	}
	ctx, cancel := context.WithTimeout(parent, backgroundCommandTimeout)
	defer cancel()
	handle := runtimeactions.ExactSessionHandle(handles.SessionID, handles.SessionGeneration)
	runtimeID, resolveErr := management.RuntimeIDForTargetRef(ctx, runtime, prepared.TargetRef)
	if resolveErr != nil || handles.SessionID < 1 || handles.SessionGeneration < 1 {
		if resolveErr == nil {
			resolveErr = errors.New("running connector action did not return an exact console session handle")
		}
		return finishRunningActionRequest(server, runtime, requestID, connectors.ResultError, nil, "", resolveErr.Error(), prepared.OutputHint)
	}
	handle.RuntimeID = runtimeID
	sessions, err := management.ConsoleSessions(runtime)
	if err != nil {
		return finishRunningActionRequest(server, runtime, requestID, connectors.ResultError, nil, "", err.Error(), prepared.OutputHint)
	}
	result, err := sessions.WaitActive(ctx, principal, handle)
	// Workspace shutdown owns the terminal transition for every in-flight
	// connector action. Leave this request running until the shutdown
	// coordinator has drained workers and marks it outcome_unknown.
	if err != nil && parent.Err() != nil {
		return nil
	}
	status := connectors.ResultStatus("")
	var output any
	var displayText string
	var errorText string
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			interruptCtx, interruptCancel := context.WithTimeout(context.Background(), finishRequestTimeout)
			interruptErr := sessions.InterruptActive(interruptCtx, principal, handle)
			interruptCancel()
			if interruptErr != nil {
				errorText = fmt.Sprintf("connector action timed out and the active console could not be interrupted: %v", interruptErr)
			} else {
				errorText = "connector action timed out while running in background"
			}
			status = connectors.ResultError
		} else {
			status = connectors.ResultError
			errorText = err.Error()
		}
	} else {
		status = connectors.ResultCompleted
		if result.ExitCode != 0 {
			status = connectors.ResultFailed
		}
		output = runtimeactions.ExecOutput(result)
		displayText = result.Output
	}
	if status == "" {
		return errors.New("finish running connector action: empty result status")
	}
	return finishRunningActionRequest(server, runtime, requestID, status, output, displayText, errorText, prepared.OutputHint)
}

type actionRequestFinisher interface {
	ConnectorFinishActionRequest(context.Context, int64, connectors.ResultStatus, any, string, string, ...connectors.OutputHint) (connectorapi.ActionRequest, error)
}

func finishRunningActionRequest(server actionRequestFinisher, _ connectorapi.ActionRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hint connectors.OutputHint) error {
	ctx, cancel := context.WithTimeout(context.Background(), finishRequestTimeout)
	defer cancel()
	_, err := server.ConnectorFinishActionRequest(ctx, requestID, status, output, displayText, errorText, hint)
	if err != nil {
		return fmt.Errorf("persist running connector action result: %w", err)
	}
	return nil
}
