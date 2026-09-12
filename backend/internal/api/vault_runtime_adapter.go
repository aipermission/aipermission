package api

import (
	"context"
	"errors"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultRuntimePorts(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.VaultRuntimePorts {
	return gatewayinfra.VaultRuntimePorts{
		InvalidateSessions: func(ctx context.Context, sessions []gatewayvault.SessionReference, scope gatewayvault.SessionMutationScope) error {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err != nil {
				return err
			}
			return lifecycle.InvalidateMutation(ctx, sessions, scope)
		},
		SessionEnvironment: func(ctx context.Context, runtimeID int64) (bool, error) {
			err := requireSessionEnvironmentCapability(ctx, s, runtime, runtimeID)
			if errors.Is(err, connectors.ErrSessionEnvironmentUnsupported) {
				return false, nil
			}
			return err == nil, err
		},
		Connector: vaultActionConnectorPort{server: s, runtime: runtime},
	}
}
