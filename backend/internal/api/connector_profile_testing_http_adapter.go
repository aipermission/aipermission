package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
)

func (s *Server) connectorProfileTestingHTTPScope(w http.ResponseWriter) (connectormanagement.ProfileTestingScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.ProfileTestingScope{}, false
	}
	return connectormanagement.ProfileTestingScope{
		Database: runtime.Storage.Database,
		Registry: runtimeConnectorRegistry(runtime),
		Runtime:  s.connectorCredentialRuntimePorts(runtime),
		SpecialTest: func(w http.ResponseWriter, r *http.Request, target connectors.TargetView, profile connectors.CredentialProfileView) bool {
			adapter := s.connectorCredentialProfileTesterFor(target.ConnectorKind)
			if adapter == nil {
				return false
			}
			adapter.TestCredentialProfile(
				connectorPeerGatewayPort{server: s}, w, r,
				connectorDataRuntimePort(runtime, target.ConnectorKind), target, profile,
			)
			return true
		},
		RedactDetails: func(ctx context.Context, details map[string]any, boundary connectormanagement.CredentialBoundary) (map[string]any, error) {
			redacted, err := s.redactedConnectorValueWithCredentialBoundary(
				ctx, runtime, details, connectorSensitiveOutputFields(), nil, boundary,
			)
			if err != nil || redacted == nil {
				return nil, err
			}
			if typed, ok := redacted.(map[string]any); ok {
				return typed, nil
			}
			return map[string]any{"value": redacted}, nil
		},
	}, true
}
