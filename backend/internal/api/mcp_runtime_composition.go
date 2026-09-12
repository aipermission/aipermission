package api

import (
	"context"

	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) mcpRuntimePorts(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.MCPRuntimePorts {
	return gatewayinfra.MCPRuntimePorts{
		StartEnabled: func(ctx context.Context) (bool, error) {
			settings, err := s.readSecuritySettings(ctx, runtime)
			return settings.MCPStartEnabled, err
		},
		StopEffects: func(ctx context.Context) error {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err != nil {
				return err
			}
			if err := lifecycle.InvalidateAll(ctx, "MCP execution stopped; send a fresh Vault request after it starts"); err != nil {
				return err
			}
			owner, err := s.vaultRequestRuntime(ctx, runtime)
			if err != nil {
				return err
			}
			if err := owner.StalePendingForAction(ctx, gatewayvault.ActionGenerateItem, "MCP execution stopped; send a fresh Vault request after it starts"); err != nil {
				return err
			}
			return owner.FailRunning(ctx, "MCP execution stopped while the Vault action was running")
		},
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	}
}
