package api

import (
	"context"
	"net/http"
	"strings"

	apihttp "github.com/aipermission/aipermission/backend/internal/api/httptransport"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) backupApplication() *gatewayinfra.BackupApplication {
	return s.operationsOwner.NewBackupApplication(gatewayinfra.BackupApplicationDependencies{
		DataPath: s.config.DataPath,
		ActiveRuntime: func(w http.ResponseWriter) (*gatewayinfra.WorkspaceHandle, gatewayinfra.BackupRuntimePorts, bool) {
			runtime, ok := s.activeRuntimeOrLocked(w)
			if !ok {
				return nil, gatewayinfra.BackupRuntimePorts{}, false
			}
			return runtime, gatewayinfra.BackupRuntimePorts{
				AuditRequired: func(ctx context.Context, action string, payload any) error {
					return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
				},
				Observe: func(ctx context.Context, action string, payload any) {
					s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
				},
			}, true
		},
		CurrentDatabaseName: s.currentDatabaseNameLocked,
		AuthorizeOperation:  s.authorizeBackupOperation,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewayinfra.PasswordAttempt, bool) {
			return s.beginDatabasePasswordAttempt(w, r)
		},
		IssuePrepared: func(w http.ResponseWriter, prepared gatewayaccess.PreparedUISession) error {
			return s.issuePreparedUISessionLocked(w, prepared)
		},
	})
}

func (s *Server) authorizeBackupOperation(w http.ResponseWriter, r *http.Request) bool {
	if !s.hasValidUISession(r) {
		writeError(w, http.StatusUnauthorized, "ui session required")
		return false
	}
	if apihttp.IsStateChangingMethod(r.Method) {
		if !s.hasValidUICSRF(r) {
			writeError(w, http.StatusForbidden, "csrf token required")
			return false
		}
		requested := strings.TrimSpace(r.Header.Get(apihttp.WorkspaceHeaderName))
		if current := strings.TrimSpace(s.currentUIWorkspaceBinding()); current == "" || requested != current {
			writeError(w, http.StatusConflict, "workspace changed; refresh before continuing")
			return false
		}
	}
	return true
}
