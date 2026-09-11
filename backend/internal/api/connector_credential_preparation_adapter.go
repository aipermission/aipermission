package api

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorCredentialPreparationPorts(runtime databaseRuntime) connectormgmt.CredentialPreparationPorts {
	return connectormgmt.RuntimeCredentialPreparation(connectorCredentialStorage(runtime), func(connectorKind string) connectormgmt.CredentialCanonicalizer {
		adapter := s.connectorCredentialCanonicalizerFor(connectorKind)
		if adapter == nil {
			return nil
		}
		return func(ctx context.Context, _, credentialKind string, public map[string]any) (map[string]any, error) {
			return adapter.CanonicalCredentialPublic(ctx, connectorDataRuntimePort(runtime, connectorKind), credentialKind, public)
		}
	})
}

func connectorCredentialStorage(runtime databaseRuntime) connectormgmt.CredentialStorage {
	if runtime == nil {
		return connectormgmt.CredentialStorage{}
	}
	return connectormgmt.CredentialStorage{
		Vault: runtime.StoragePort().SecretVault(), WorkspaceID: runtime.WorkspaceIdentifier(),
	}
}
