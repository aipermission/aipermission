package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (s *Server) connectorCredentialRuntimePorts(runtime *databaseRuntime) connectormanagement.CredentialRuntimePorts {
	return connectormanagement.RuntimeCredentialPorts(
		runtime,
		func(kind string) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
		func(ctx context.Context, result connectors.ActionResult, boundary connectormanagement.CredentialBoundary) (connectors.ActionResult, error) {
			return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, boundary)
		},
		func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	)
}
