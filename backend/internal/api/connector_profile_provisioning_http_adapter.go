package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

type provisionConnectorCredentialProfileRequest = connectormanagement.ProvisionRequest

func (s *Server) connectorProfileProvisioningHTTPScope(w http.ResponseWriter) (connectormanagement.ProvisioningScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.ProvisioningScope{}, false
	}
	return connectormanagement.ProvisioningScope{
		Database: runtime.database,
		Registry: runtime.connectorRegistry(),
		Runtime:  s.connectorCredentialRuntimePorts(runtime),
		EncryptSecret: func(_ context.Context, profileID int64, secret json.RawMessage) (string, error) {
			return recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, secret)
		},
		WithTransaction: func(ctx context.Context, mutate func(*sql.Tx, connectormanagement.AuditAppender) error) error {
			return s.withAuditedTransaction(ctx, runtime, func(tx *sql.Tx, appendAudit auditAppender) error {
				return mutate(tx, connectormanagement.AuditAppender(appendAudit))
			})
		},
		EnsureRuntimeSurfaces: func(ctx context.Context, store *connectortargets.Store, target connectortargets.Target, profile connectortargets.CredentialProfile) error {
			return s.ensureConnectorRuntimeSurfacesForProfile(ctx, store, target, profile)
		},
		AuditRequired: func(ctx context.Context, action string, payload any) error {
			return s.writeAuditRequired(ctx, runtime, "gateway", nil, 0, action, payload)
		},
	}, true
}
