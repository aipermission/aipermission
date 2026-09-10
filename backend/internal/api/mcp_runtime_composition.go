package api

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/vaultrequests"
)

func (s *Server) mcpRuntimeHTTPScope(w http.ResponseWriter) (runtimecontrol.MCPRuntimeScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return runtimecontrol.MCPRuntimeScope{}, false
	}
	return runtimecontrol.MCPRuntimeScope{
		State: &runtime.runtimeState,
		StartEnabled: func(ctx context.Context) (bool, error) {
			settings, err := readSecuritySettings(ctx, runtime)
			return settings.MCPStartEnabled, err
		},
		AcquireStop: runtime.vaultDelivery.AcquireExclusive,
		StopEffects: func(ctx context.Context) error {
			if err := s.invalidateAllVaultSessions(ctx, runtime, "MCP execution stopped; send a fresh Vault request after it starts"); err != nil {
				return err
			}
			owner, err := s.vaultRequestRuntime(ctx, runtime)
			if err != nil {
				return err
			}
			if err := owner.StalePendingForAction(ctx, vaultrequests.ActionGenerateItem, "MCP execution stopped; send a fresh Vault request after it starts"); err != nil {
				return err
			}
			return owner.FailRunning(ctx, "MCP execution stopped while the Vault action was running")
		},
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	}, true
}

func (runtime *databaseRuntime) isMCPStarted() bool {
	return runtime != nil && runtime.runtimeState.MCPStarted()
}

func (runtime *databaseRuntime) setMCPStarted(enabled bool) {
	if runtime != nil {
		runtime.runtimeState.SetMCPStarted(enabled)
	}
}
