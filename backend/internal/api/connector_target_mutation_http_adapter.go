package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s *Server) connectorTargetMutationHTTPScope(w http.ResponseWriter) (connectormanagement.TargetMutationScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.TargetMutationScope{}, false
	}
	return s.connectorTargetMutationScope(runtime), true
}

func (s *Server) connectorTargetMutationScope(runtime *databaseRuntime) connectormanagement.TargetMutationScope {
	return connectormanagement.TargetMutationScope{
		Database: runtime.Storage.Database,
		Registry: runtimeConnectorRegistry(runtime),
		ValidateTransport: func(ctx context.Context, projectID int64, config map[string]any) error {
			return s.validateConnectorTransportConfig(ctx, connectortargets.NewStore(runtime.Storage.Database), projectID, config)
		},
		AcquireExclusive: runtime.Security.VaultDelivery.AcquireExclusive,
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, connectormanagement.AuditAppender(appendAudit))
			})
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return s.ensureConnectorRuntimeSurfacesForProfile(ctx, store, target, profile)
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return (connectorTargetHandlers{s}).afterConnectorCredentialLifecycleChange(
				ctx, runtime, change.TargetID, change.ProfileID, change.StaleReason,
				change.UserMessage, change.IncludeRunning,
			)
		},
	}
}
