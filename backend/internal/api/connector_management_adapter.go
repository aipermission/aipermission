package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectormanagement"
)

func (s *Server) connectorManagementScope(w http.ResponseWriter) (connectormanagement.Scope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return connectormanagement.Scope{}, false
	}
	return connectormanagement.Scope{
		Database: runtime.database,
		Registry: runtime.connectorRegistry(),
		Features: func(kind string) connectormanagement.ConnectorFeatures {
			features := connectormanagement.ConnectorFeatures{
				FileTransfer: s.connectorFileTransferAdapterFor(kind) != nil,
			}
			if adapter := s.connectorLiveConsoleTargetAdapterFor(kind); adapter != nil {
				features.LiveConsoleCapability = adapter.LiveConsoleCapabilityKind()
			}
			return features
		},
		SessionEnvironmentSupported: func(ctx context.Context, runtimeID int64) bool {
			return requireSessionEnvironmentCapability(ctx, s, runtime, runtimeID) == nil
		},
	}, true
}
