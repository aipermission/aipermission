package api

import (
	"context"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
)

func (s *Server) connectorCredentialRuntimePorts(runtime databaseRuntime) connectormgmt.CredentialRuntimePorts {
	return connectormgmt.RuntimeCredentialPorts(
		connectorCredentialStorage(runtime),
		func(kind string) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
		func(ctx context.Context, result connectors.ActionResult, boundary connectormgmt.CredentialBoundary) (connectors.ActionResult, error) {
			return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, boundary)
		},
		func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	)
}
