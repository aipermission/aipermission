package api

import (
	"context"
	"log"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	actions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
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

func (accessor connectorSecretAccessor) GetSecret(ctx context.Context, name string) (string, error) {
	return (actions.SecretAccessor{Values: accessor.values, Boundary: accessor.boundary}).GetSecret(ctx, name)
}

func (accessor connectorSecretAccessor) RegisterSensitiveValue(value string) {
	accessor.boundary.Add(value)
}

type noopConnectorEventSink = actions.NoopEventSink

func (s *Server) callConnectorAction(ctx context.Context, runtime databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	return s.connectorActionApplication().Call(ctx, s.connectorActionWorkspace(runtime), call)
}

func (s *Server) runLocalConnectorAction(ctx context.Context, runtime databaseRuntime, call connectorActionCall) (connectorActionCallResult, error) {
	return s.connectorActionApplication().RunLocal(ctx, s.connectorActionWorkspace(runtime), call)
}

func (s *Server) finishActiveConnectorActionRequest(runtime databaseRuntime, requestID int64, prepared actions.PreparedRequest, principal gatewayaccess.Principal, handles connectors.ActionHandles) {
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	if adapter == nil || !adapter.SupportsRunning(prepared) {
		return
	}
	gatewayPort, runtimePort := s.connectorPortsApplication().ActionFinishPorts(s.connectorPortsWorkspace(runtime), prepared.Target.ConnectorKind)
	if err := adapter.FinishRunning(gatewayPort, runtimePort, requestID, prepared, principal, handles); err != nil {
		log.Printf("finish running connector action failed connector=%q request=%d error=%v", prepared.Target.ConnectorKind, requestID, err)
	}
}

func (s *Server) connectorActionSupportsRunning(prepared actions.PreparedRequest) bool {
	adapter := s.connectorRuntimeAdapterFor(prepared.Target.ConnectorKind)
	return adapter != nil && adapter.SupportsRunning(prepared)
}

func (s *Server) finishConnectorActionRequest(ctx context.Context, runtime databaseRuntime, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
	return s.connectorActionApplication().Finish(ctx, s.connectorActionWorkspace(runtime), requestID, status, output, displayText, errorText, hints...)
}
