package api

import (
	"context"
	"database/sql"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewaybackup "github.com/aipermission/aipermission/backend/internal/gatewayoperations/backup"
)

func (s *Server) backupApplication() *gatewaybackup.Component {
	return gatewaybackup.New(gatewaybackup.Dependencies{
		DataPath: s.config.DataPath, Lifecycle: s.infrastructure.WorkspaceLifecycle(),
		ActiveRuntime: func(w http.ResponseWriter) (gatewaybackup.Runtime, bool) {
			runtime, ok := s.activeRuntimeOrLocked(w)
			if !ok {
				return gatewaybackup.Runtime{}, false
			}
			return gatewaybackup.Runtime{
				Database: runtime.Storage.DatabaseHandle(), SecretVault: runtime.Storage.SecretVault(),
				DatabaseID: runtime.Identity.DatabaseID, DatabasePath: runtime.Identity.DatabasePath, WorkspaceID: runtime.Identity.WorkspaceID,
				Mutate: func(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
					return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
				},
				AuditRequired: func(ctx context.Context, action string, payload any) error {
					return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
				},
				Observe: func(ctx context.Context, action string, payload any) {
					s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
				},
			}, true
		},
		CurrentDatabaseName: s.currentDatabaseNameLocked,
		HasSession:          s.hasValidUISession,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewaybackup.PasswordAttempt, bool) {
			return s.beginDatabasePasswordAttempt(w, r)
		},
		IssuePrepared: func(w http.ResponseWriter, prepared gatewayaccess.PreparedUISession) error {
			return s.issuePreparedUISessionLocked(w, prepared)
		},
		AcquireOperation: s.infrastructure.AcquireBackupOperation,
	})
}
