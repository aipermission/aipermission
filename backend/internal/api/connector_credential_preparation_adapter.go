package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/recordcrypto"
)

func (s *Server) connectorCredentialPreparationPorts(runtime *databaseRuntime) connectormanagement.CredentialPreparationPorts {
	return connectormanagement.CredentialPreparationPorts{
		Canonicalize: func(ctx context.Context, connectorKind, credentialKind string, public map[string]any) (map[string]any, error) {
			if adapter := s.connectorCredentialCanonicalizerFor(connectorKind); adapter != nil {
				return adapter.CanonicalCredentialPublic(ctx, connectorDataRuntimePort(runtime, connectorKind), credentialKind, public)
			}
			if public == nil {
				return map[string]any{}, nil
			}
			copied := make(map[string]any, len(public))
			for key, value := range public {
				copied[key] = value
			}
			return copied, nil
		},
		Decrypt: func(_ context.Context, profileID int64, encrypted string) (map[string]any, error) {
			secret := map[string]any{}
			err := recordcrypto.DecryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, encrypted, &secret)
			return secret, err
		},
		Encrypt: func(_ context.Context, profileID int64, secret map[string]any) (string, error) {
			return recordcrypto.EncryptJSON(runtime.Storage.Vault, runtime.WorkspaceUUID, recordcrypto.ConnectorCredentialProfile, profileID, secret)
		},
	}
}
