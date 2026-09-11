package api

import (
	"context"
	"net/http"

	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

func (s *Server) maintenanceConsoleHTTPScope(w http.ResponseWriter) (gatewayoperations.MaintenanceHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayoperations.MaintenanceHTTPScope{}, false
	}
	return gatewayoperations.MaintenanceHTTPScope{
		Runtime: s.infrastructure.MaintenanceConsole(),
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
		UpgradeWebSocket: s.upgradeWebSocket,
	}, true
}

func (s *Server) closeMaintenanceConsoleForLifecycle(reason string) bool {
	if s == nil || s.infrastructure == nil || s.infrastructure.MaintenanceConsole() == nil || !s.infrastructure.MaintenanceConsole().Close() {
		return false
	}
	if runtime := s.activeRuntime(); runtime != nil {
		s.writeObservationAudit(context.Background(), runtime, "system", nil, 0, "maintenance_console.closed", map[string]any{
			"scope": "local-ui-only", "mode": "realtime-pty", "closed": true, "reason": reason,
		})
	}
	return true
}
