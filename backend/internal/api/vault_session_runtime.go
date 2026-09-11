package api

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) configureVaultSessionRuntime(runtime databaseRuntime) error {
	owner, err := s.vaultSessionLifecycle(runtime)
	if err != nil {
		return err
	}
	return owner.Configure()
}

func (s *Server) vaultSessionLifecycle(runtime databaseRuntime) (*gatewayvault.SessionLifecycle, error) {
	if s == nil || runtime == nil || runtime.StoragePort().DatabaseHandle() == nil ||
		runtime.SecurityPort().VaultLeaseStore() == nil || runtime.ConnectorPort().ConsoleSessionManager() == nil {
		return nil, gatewayvault.ErrInvalidatorUnavailable
	}
	delivery := runtime.SecurityPort().VaultDeliveryCoordinator()
	return s.vaultApplication().SessionLifecycle(gatewayvault.SessionLifecycleRuntime{
		Database: runtime.StoragePort().DatabaseHandle(),
		Leases:   runtime.SecurityPort().VaultLeaseStore(),
		Sessions: runtime.ConnectorPort().ConsoleSessionManager(),
		Principal: func() (gatewayaccess.Principal, error) {
			return s.localExecutionPrincipal(runtime)
		},
		Requests: func(ctx context.Context) (gatewayvault.RequestInvalidator, error) {
			owner, err := s.vaultRequestRuntime(ctx, runtime)
			if err != nil {
				return nil, err
			}
			return owner, nil
		},
		AcquireDelivery: delivery.AcquireDelivery,
		InstallAuthorizer: func(guard gatewayvault.SessionAuthorizationGuard) {
			runtime.ConnectorPort().ConsoleSessionManager().SetAuthorizer(func(
				ctx context.Context,
				principal gatewayaccess.Principal,
				session gatewayoperations.SessionAuthorization,
				operation gatewayoperations.SessionOperation,
				run func() error,
			) error {
				return guard(ctx, func() error {
					return runtime.SecurityPort().VaultLeaseStore().Authorize(ctx, principal, session, operation)
				}, run)
			})
		},
		InstallSessionClosed: func(hook func(gatewayvault.VaultSessionReference)) {
			runtime.ConnectorPort().ConsoleSessionManager().SetSessionClosedHook(func(handle gatewayoperations.SessionHandle) {
				hook(gatewayvault.VaultSessionReference{
					SessionID: handle.ID, RuntimeID: handle.RuntimeID, Generation: handle.Generation,
				})
			})
		},
	})
}
