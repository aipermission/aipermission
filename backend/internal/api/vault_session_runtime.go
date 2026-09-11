package api

import (
	"context"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) configureVaultSessionRuntime(runtime databaseRuntime) error {
	if runtime == nil || runtime.ConnectorPort().ConsoleSessionManager() == nil || runtime.SecurityPort().VaultLeaseStore() == nil {
		return gatewayvault.ErrInvalidatorUnavailable
	}
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	runtime.ConnectorPort().ConsoleSessionManager().SetAuthorizer(func(
		ctx context.Context,
		principal gatewayaccess.Principal,
		session gatewayoperations.SessionAuthorization,
		operation gatewayoperations.SessionOperation,
		run func() error,
	) error {
		release, err := runtime.SecurityPort().VaultDeliveryCoordinator().AcquireDelivery(ctx)
		if err != nil {
			return err
		}
		defer release()
		if err := runtime.SecurityPort().VaultLeaseStore().Authorize(ctx, principal, session, operation); err != nil {
			return err
		}
		return run()
	})
	runtime.ConnectorPort().ConsoleSessionManager().SetSessionClosedHook(func(handle gatewayoperations.SessionHandle) {
		_ = owner.SessionClosed(context.Background(), handle)
	})
	return nil
}
