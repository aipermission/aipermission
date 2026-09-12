package api

import (
	"context"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorLifecycleApplication(runtime *gatewayinfra.WorkspaceHandle) *connectormgmt.LifecycleService {
	return connectormgmt.NewLifecycleService(connectormgmt.LifecycleServiceDependencies{
		Mutate: s.connectorManagementOwner.LifecycleMutationRunner(runtime),
		Redact: func(ctx context.Context, value string) string {
			return s.redactForPersistence(ctx, runtime, value)
		},
		InvalidateVault: func(ctx context.Context, targetID, profileID int64, reason string) error {
			lifecycle, err := s.vaultSessionLifecycle(runtime)
			if err != nil {
				return err
			}
			return lifecycle.InvalidateTargetProfile(ctx, targetID, profileID, reason)
		},
	})
}
