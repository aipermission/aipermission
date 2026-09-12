package api

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func (s *Server) mcpRuntimePorts(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.MCPRuntimePorts {
	return gatewayinfra.MCPRuntimePorts{
		StartEnabled: func(ctx context.Context) (bool, error) {
			settings, err := s.readSecuritySettings(ctx, runtime)
			return settings.MCPStartEnabled, err
		},
		StopEffects: func(ctx context.Context) error {
			return s.vaultOwner.StopMCP(ctx, runtime, s.vaultApplication(), s.vaultRuntimePorts(runtime), gatewayinfra.VaultSessionPorts{
				Principal: func() (gatewayaccess.Principal, error) {
					return s.localExecutionPrincipal(runtime)
				},
			})
		},
		Observe: func(ctx context.Context, action string, payload map[string]any) {
			s.writeObservationAudit(ctx, runtime, "user", nil, 0, action, payload)
		},
	}
}
