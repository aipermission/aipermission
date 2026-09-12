package api

import (
	"context"
	"net/http"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) connectorApprovalHTTPScope(w http.ResponseWriter) (connectormgmt.ConnectorApprovalScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormgmt.ConnectorApprovalScope{}, false
	}
	return s.connectorActionOwner.ConnectorApprovalWorkspace(runtime, gatewayinfra.ConnectorApprovalPorts{
		Workflow: func() (connectormgmt.ConnectorApprovalWorkflow, error) {
			return s.connectorActionApprovalWorkflow(runtime)
		},
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	})
}
