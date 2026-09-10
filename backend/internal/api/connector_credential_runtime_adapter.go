package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func (s *Server) connectorCredentialRuntimePorts(runtime *databaseRuntime) connectormanagement.CredentialRuntimePorts {
	return connectormanagement.CredentialRuntimePorts{
		DecryptSecret: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			secret := map[string]any{}
			err := recordcrypto.DecryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, encrypted, &secret)
			return secret, err
		},
		RuntimeContext: func(target connectortargets.Target, profile connectortargets.CredentialProfile, secrets map[string]any, boundary connectormanagement.CredentialBoundary) connectors.RuntimeContext {
			return connectors.RuntimeContext{
				Target: connectorTargetViewForProfile(target, profile.ID), Profile: connectortargets.CredentialProfileView(profile),
				Secrets: connectorSecretAccessor{values: secrets, boundary: boundary}, Events: noopConnectorEventSink{},
				Capabilities: connectorRuntimeCapabilitiesFor(target.ConnectorKind, s, runtime),
			}
		},
		RedactResult: func(ctx context.Context, result connectors.ActionResult, boundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
			return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, boundary)
		},
		RedactText: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	}
}
