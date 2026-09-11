package api

import (
	"context"
	"database/sql"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

type importDatabaseRequest = gatewayoperations.ImportDatabaseRequest
type transientBackupRestoreRequest = gatewayoperations.TransientRestoreRequest

func (s *Server) backupApplication() *gatewayoperations.BackupApplication {
	return gatewayoperations.NewBackupApplication(gatewayoperations.BackupDependencies{
		DataPath: s.config.DataPath, Lifecycle: s.infrastructure.WorkspaceLifecycle(),
		ActiveRuntime: func(w http.ResponseWriter) (gatewayoperations.BackupRuntime, bool) {
			runtime, ok := s.activeRuntimeOrLocked(w)
			if !ok {
				return gatewayoperations.BackupRuntime{}, false
			}
			return gatewayoperations.BackupRuntime{
				Database: runtime.StoragePort().DatabaseHandle(), SecretVault: runtime.StoragePort().SecretVault(),
				DatabaseID: runtime.DatabaseIdentifier(), DatabasePath: runtime.DatabasePath(), WorkspaceID: runtime.WorkspaceIdentifier(),
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
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewayoperations.BackupPasswordAttempt, bool) {
			return s.beginDatabasePasswordAttempt(w, r)
		},
		IssuePrepared: func(w http.ResponseWriter, prepared gatewayaccess.PreparedUISession) error {
			return s.issuePreparedUISessionLocked(w, prepared)
		},
		AcquireOperation: s.infrastructure.AcquireBackupOperation,
	})
}
