package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/applicationbackup"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type importDatabaseRequest = applicationbackup.ImportDatabaseRequest
type transientBackupRestoreRequest = applicationbackup.TransientRestoreRequest

func (s *Server) backupApplication() *applicationbackup.Component {
	return applicationbackup.New(applicationbackup.Dependencies{
		DataPath: s.config.DataPath, Lifecycle: s.workspaceState.Lifecycle,
		ActiveRuntime: s.activeRuntimeOrLocked, CurrentDatabaseName: s.currentDatabaseNameLocked,
		HasSession: s.hasValidUISession,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (applicationbackup.PasswordAttempt, bool) {
			return s.beginDatabasePasswordAttempt(w, r)
		},
		IssuePrepared: func(w http.ResponseWriter, prepared uisession.Prepared) error {
			return s.issuePreparedUISessionLocked(w, prepared)
		},
		AcquireOperation: s.controlState.BackupOperations.Acquire,
		Mutate: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AuditRequired: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload any) error {
			return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
		},
		Observe: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	})
}
