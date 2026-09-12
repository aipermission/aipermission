package api

import (
	"context"
	"fmt"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

type connectorActionCall = gatewayactions.Call
type connectorActionCallResult = gatewayactions.CallResult

type connectorSecretAccessor struct {
	values   map[string]any
	boundary interface{ Add(...string) }
}

func (accessor connectorSecretAccessor) GetSecret(_ context.Context, name string) (string, error) {
	value, ok := accessor.values[name]
	if !ok || value == nil {
		return "", fmt.Errorf("%w: %q", connectors.ErrSecretNotFound, name)
	}
	text := fmt.Sprint(value)
	if accessor.boundary != nil {
		accessor.boundary.Add(text)
	}
	return text, nil
}

func (accessor connectorSecretAccessor) RegisterSensitiveValue(value string) {
	if accessor.boundary != nil {
		accessor.boundary.Add(value)
	}
}

type noopConnectorEventSink struct{}

func (noopConnectorEventSink) Emit(context.Context, connectors.ActionEvent) error { return nil }

func (s *Server) callConnectorAction(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, call connectorActionCall) (connectorActionCallResult, error) {
	return s.connectorActions.Call(ctx, runtime, call)
}

func (s *Server) runLocalConnectorAction(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, call connectorActionCall) (connectorActionCallResult, error) {
	return s.connectorActions.RunLocal(ctx, runtime, call)
}

func (s *Server) finishConnectorActionRequest(ctx context.Context, runtime *gatewayinfra.WorkspaceHandle, requestID int64, status connectors.ResultStatus, output any, displayText string, errorText string, hints ...connectors.OutputHint) (connectormgmt.ActionRequest, error) {
	return s.connectorActions.Finish(ctx, runtime, requestID, status, output, displayText, errorText, hints...)
}
