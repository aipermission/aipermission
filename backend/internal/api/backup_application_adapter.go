package api

import (
	"context"
	"database/sql"
	"net/http"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type importDatabaseRequest = gatewayoperations.ImportDatabaseRequest
type transientBackupRestoreRequest = gatewayoperations.TransientRestoreRequest

func (s *Server) backupApplication() *gatewayoperations.BackupApplication {
	return gatewayoperations.NewBackupApplication(gatewayoperations.BackupDependencies{
		DataPath: s.config.DataPath, Lifecycle: s.workspaceState.Lifecycle,
		ActiveRuntime: s.activeRuntimeOrLocked, CurrentDatabaseName: s.currentDatabaseNameLocked,
		HasSession: s.hasValidUISession,
		BeginAttempt: func(w http.ResponseWriter, r *http.Request) (gatewayoperations.BackupPasswordAttempt, bool) {
			return s.beginDatabasePasswordAttempt(w, r)
		},
		IssuePrepared: func(w http.ResponseWriter, prepared gatewayvault.PreparedUISession) error {
			return s.issuePreparedUISessionLocked(w, prepared)
		},
		AcquireOperation: s.controlState.BackupOperations.Acquire,
		Mutate: func(ctx context.Context, runtime *gatewayinfra.Runtime, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		AuditRequired: func(ctx context.Context, runtime *gatewayinfra.Runtime, action string, payload any) error {
			return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
		},
		Observe: func(ctx context.Context, runtime *gatewayinfra.Runtime, action string, payload any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	})
}
