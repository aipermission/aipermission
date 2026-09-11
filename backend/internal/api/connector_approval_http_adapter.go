package api

import (
	"context"
	"net/http"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorApprovalHTTPScope(w http.ResponseWriter) (connectormgmt.ConnectorApprovalScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormgmt.ConnectorApprovalScope{}, false
	}
	return connectormgmt.ConnectorApprovalScope{
		Database: runtime.Storage.DatabaseHandle(),
		Workflow: func() (connectormgmt.ConnectorApprovalWorkflow, error) {
			return s.connectorActionApprovalWorkflow(runtime)
		},
		MCPStarted: func() bool { return runtime.Security.RuntimeControlState().MCPStarted() },
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	}, true
}
