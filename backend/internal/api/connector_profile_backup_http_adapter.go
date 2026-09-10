package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
)

func (s *Server) connectorProfileBackupHTTPScope(w http.ResponseWriter) (connectormanagement.ProfileBackupScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.ProfileBackupScope{}, false
	}
	return connectormanagement.ProfileBackupScope{
		Database: runtime.database, Registry: runtime.connectorRegistry(),
		Runtime: s.connectorCredentialRuntimePorts(runtime),
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	}, true
}
