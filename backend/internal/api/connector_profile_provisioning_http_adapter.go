package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/actionresult"
	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
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
		DecryptSecret: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			secret := map[string]any{}
			err := recordcrypto.DecryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, encrypted, &secret)
			return secret, err
		},
		EncryptSecret: func(_ context.Context, profileID int64, secret json.RawMessage) (string, error) {
			return recordcrypto.EncryptJSON(runtime.vault, runtime.workspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, secret)
		},
		RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary actionresult.CredentialBoundary) connectors.RuntimeContext {
			return connectors.RuntimeContext{
				Target: connectorTargetViewForProfile(target, profile.ID), Profile: connectortargets.CredentialProfileView(profile),
				Secrets: connectorSecretAccessor{values: secrets, boundary: boundary}, Events: noopConnectorEventSink{},
				Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s, runtime),
			}
		},
		RedactResult: func(ctx context.Context, result connectors.ActionResult, boundary actionresult.CredentialBoundary) (connectors.ActionResult, error) {
			return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, boundary)
		},
		RedactText: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
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
