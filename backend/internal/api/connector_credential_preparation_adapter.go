package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorCredentialPreparationPorts(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.CredentialPreparationPorts {
	return s.connectorManagementApplication().RuntimeCredentialPreparation(s.connectorCredentialStorage(runtime), func(connectorKind string) connectormgmt.CredentialCanonicalizer {
		adapter := s.connectorCredentialCanonicalizerFor(connectorKind)
		if adapter == nil {
			return nil
		}
		return func(ctx context.Context, _, credentialKind string, public map[string]any) (map[string]any, error) {
			return adapter.CanonicalCredentialPublic(ctx, s.connectorDataRuntimePort(runtime, connectorKind), credentialKind, public)
		}
	})
}

func (s *Server) connectorCredentialStorage(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.CredentialStorage {
	if s == nil || s.infrastructure == nil {
		return connectormgmt.CredentialStorage{}
	}
	storage, _ := s.infrastructure.ConnectorCredentialStorage(runtime)
	return storage
}
