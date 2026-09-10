package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
)

func (s *Server) connectorCredentialPreparationPorts(runtime *databaseRuntime) connectormanagement.CredentialPreparationPorts {
	return connectormanagement.RuntimeCredentialPreparation(runtime, func(connectorKind string) connectormanagement.CredentialCanonicalizer {
		adapter := s.connectorCredentialCanonicalizerFor(connectorKind)
		if adapter == nil {
			return nil
		}
		return func(ctx context.Context, _, credentialKind string, public map[string]any) (map[string]any, error) {
			return adapter.CanonicalCredentialPublic(ctx, connectorDataRuntimePort(runtime, connectorKind), credentialKind, public)
		}
	})
}
