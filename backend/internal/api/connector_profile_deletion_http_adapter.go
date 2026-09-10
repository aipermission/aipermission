package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s *Server) connectorProfileDeletionHTTPScope(w http.ResponseWriter) (connectormanagement.ProfileDeletionScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.ProfileDeletionScope{}, false
	}
	return connectormanagement.ProfileDeletionScope{
		Database:         runtime.database,
		AcquireExclusive: runtime.vaultDelivery.AcquireExclusive,
		Cleanup: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) (connectormanagement.ProfileCleanupOutcome, error) {
			return connectormanagement.CleanupProvisionedCredentialProfileIfNeeded(ctx, connectormanagement.ManagedCredentialCleanupScope{
				Database: runtime.database, Registry: runtime.connectorRegistry(), Runtime: s.connectorCredentialRuntimePorts(runtime),
			}, target, profile)
		},
		BeforeDelete: func(ctx context.Context, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			if adapter := s.connectorCredentialProfileLifecycleAdapterFor(target.ConnectorKind); adapter != nil {
				gateway := connectorRuntimeActionGatewayPort{connectorPeerGatewayPort: connectorPeerGatewayPort{server: s}, runtime: runtime, kind: target.ConnectorKind}
				return adapter.BeforeDeleteCredentialProfile(ctx, gateway, connectorTargetLifecycleRuntime(runtime, target.ConnectorKind), target, profile)
			}
			return nil
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, connectormanagement.AuditAppender(appendAudit))
			})
		},
		AfterLifecycleChange: func(ctx context.Context, change connectormanagement.TargetLifecycleChange) error {
			return (connectorTargetHandlers{s}).afterConnectorCredentialLifecycleChange(
				ctx, runtime, change.TargetID, change.ProfileID, change.StaleReason,
				change.UserMessage, change.IncludeRunning,
			)
		},
	}, true
}
