package api

import (
	"context"
	"database/sql"

	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
)

func (s *Server) connectorLifecycleApplication(runtime databaseRuntime) *connectormgmt.LifecycleService {
	return connectormgmt.NewLifecycleService(connectormgmt.LifecycleServiceDependencies{
		Mutate: func(
			ctx context.Context,
			actor string,
			action string,
			payload func() any,
			mutate func(*sql.Tx) error,
		) error {
			return s.withAuditedMutation(ctx, runtime, actor, nil, 0, action, payload, mutate)
		},
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
