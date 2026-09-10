package api

import (
	"context"
	"fmt"
	"log"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

var errMCPExecutionStopped = actions.ErrMCPExecutionStopped

const (
	connectorActionApprovalHint     = actions.ApprovalHint
	connectorActionRunningHint      = actions.RunningHint
	connectorActionPersistenceError = actions.TerminalPersistenceErrorText
)

type connectorActionCall = actions.Call
type connectorActionCallResult = actions.CallResult
type connectorActionTerminalPersistenceError = actions.TerminalPersistenceError

type connectorSecretAccessor struct {
	values   map[string]any
	boundary connectorCredentialBoundary
}

func (a connectorSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := a.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	a.boundary.Add(text)
	return text, nil
}

func (a connectorSecretAccessor) RegisterSensitiveValue(value string) { a.boundary.Add(value) }

type noopConnectorEventSink struct{}

func (noopConnectorEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

func (s *Server) callConnectorAction(ctx context.Context, runtime *databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	return workflow.Call(ctx, call)
}

func (s *Server) runLocalConnectorAction(ctx context.Context, runtime *databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectorActionCallResult{}, err
	}
	return workflow.RunLocal(ctx, call)
}

func (s *Server) finishActiveConnectorActionRequest(runtime *databaseRuntime, requestID int64, prepared actions.PreparedRequest, principal executionprincipal.Principal, handles connectors.ActionHandles) {
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	if adapter == nil || !adapter.SupportsRunning(prepared) {
		return
	}
	gatewayPort, runtimePort := newActionFinishPorts(s, runtime, prepared.Target.ConnectorKind)
	if err := adapter.FinishRunning(gatewayPort, runtimePort, requestID, prepared, principal, handles); err != nil {
		log.Printf("finish running connector action failed connector=%q request=%d error=%v", prepared.Target.ConnectorKind, requestID, err)
	}
}

func (s *Server) connectorActionSupportsRunning(prepared actions.PreparedRequest) bool {
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	return adapter != nil && adapter.SupportsRunning(prepared)
}

func (s *Server) finishConnectorActionRequest(ctx context.Context, runtime *databaseRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectortargets.ActionRequest, error) {
	workflow, err := s.connectorActionWorkflow(runtime)
	if err != nil {
		return connectortargets.ActionRequest{}, err
	}
	return workflow.Finish(ctx, requestID, status, output, displayText, errorText, hints...)
}
