package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s *Server) connectorProfileMutationHTTPScope(w http.ResponseWriter) (connectormanagement.ProfileMutationScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.ProfileMutationScope{}, false
	}
	return s.connectorProfileMutationScope(runtime), true
}

func (s *Server) connectorProfileMutationScope(runtime *databaseRuntime) connectormanagement.ProfileMutationScope {
	return connectormanagement.ProfileMutationScope{
		Database:         runtime.database,
		Registry:         runtime.connectorRegistry(),
		Preparation:      s.connectorCredentialPreparationPorts(runtime),
		AcquireExclusive: runtime.vaultDelivery.acquireExclusive,
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, connectormanagement.AuditAppender(appendAudit))
			})
		},
		BeforeCreate: func(ctx context.Context, target connectortargets.Target) error {
			if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
				return adapter.BeforeCreateCredentialProfile(ctx, connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target)
			}
			return nil
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
