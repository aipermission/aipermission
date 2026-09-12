package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) bulkCommandHTTPScope(w http.ResponseWriter) (*gatewayoperations.CommandBulkHTTPRuntime, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return nil, false
	}
	requests, err := s.commandRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return nil, false
	}
	return s.operationsOwner.CommandBulkRuntime(runtime, gatewayoperations.CommandBulkHTTPRuntime{
		Requests: requests,
		Principal: func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		},
		ResolveTarget: func(ctx context.Context, runtimeID int64) (gatewayoperations.CommandBulkTarget, error) {
			return s.connectorManagementApplication().ResolveBulkCommandTarget(ctx, runtime, s.connectorKinds(), runtimeID)
		},
		PresentError: func(ctx context.Context, runtimeID int64, err error) string {
			adapter := s.connectorManagementApplication().ConsoleErrorPresenter(ctx, runtime, s.connectorKinds(), runtimeID)
			return connectorapi.PresentedErrorMessage(adapter, "command execution failed", err)
		},
		InitialTimeout: mcpInitialExecTimeout,
	})
}
