package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectorapproval"
)

func (s *Server) connectorApprovalHTTPScope(w http.ResponseWriter) (connectorapproval.Scope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectorapproval.Scope{}, false
	}
	return connectorapproval.Scope{
		Database: runtime.database,
		Workflow: func() (connectorapproval.Workflow, error) {
			return s.connectorActionWorkflow(runtime)
		},
		MCPStarted: runtime.isMCPStarted,
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	}, true
}
