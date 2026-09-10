package api

import (
	"context"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func (s *Server) connectorApprovalHTTPScope(w http.ResponseWriter) (gatewayaccess.ConnectorApprovalScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayaccess.ConnectorApprovalScope{}, false
	}
	return gatewayaccess.ConnectorApprovalScope{
		Database: runtime.Storage.Database,
		Workflow: func() (gatewayaccess.ConnectorApprovalWorkflow, error) {
			return s.connectorActionWorkflow(runtime)
		},
		MCPStarted: runtime.IsMCPStarted,
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	}, true
}
