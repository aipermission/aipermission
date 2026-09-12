package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorCredentialRuntimePorts(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.CredentialRuntimePorts {
	return s.connectorManagementApplication().RuntimeCredentialPorts(
		s.connectorCredentialStorage(runtime),
		func(kind string) connectors.RuntimeCapabilityResolver {
			return connectorRuntimeCapabilitiesFor(kind, s, runtime)
		},
		func(ctx context.Context, result connectors.ActionResult, boundary connectormgmt.CredentialBoundary) (connectors.ActionResult, error) {
			return s.redactConnectorActionResultWithCredentialBoundary(ctx, runtime, result, gatewayactions.AdoptCredentialBoundary(boundary))
		},
		func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
	)
}
