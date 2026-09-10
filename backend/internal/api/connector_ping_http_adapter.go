package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

func (s *Server) connectorPingHTTPScope(w http.ResponseWriter) (connectormanagement.HostPingScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.HostPingScope{}, false
	}
	transport := connectorNetworkTransport{server: s, runtime: runtime}
	return connectormanagement.HostPingScope{
		ValidateTransport: func(ctx context.Context, projectID int64, mode, targetRef string) error {
			return s.validateConnectorTransportConfig(ctx, connectortargets.NewStore(runtime.database), projectID, map[string]any{
				"connection_mode": mode, "transport_target_ref": targetRef,
			})
		},
		Probe: func(ctx context.Context, request connectors.NetworkDialRequest) error {
			connection, err := transport.DialConnectorTCP(ctx, request)
			if err != nil {
				return err
			}
			if connection == nil {
				return errors.New("connector transport returned no connection")
			}
			_ = connection.Close()
			return nil
		},
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	}, true
}
