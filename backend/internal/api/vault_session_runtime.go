package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func (s *Server) configureVaultSessionRuntime(runtime *databaseRuntime) error {
	if runtime == nil || runtime.consoleSessions == nil || runtime.vaultLeases == nil {
		return vaultsessions.ErrInvalidatorUnavailable
	}
	owner, err := s.vaultSessionInvalidator(runtime)
	if err != nil {
		return err
	}
	runtime.consoleSessions.SetAuthorizer(func(
		ctx context.Context,
		principal executionprincipal.Principal,
		session console.SessionAuthorization,
		operation console.SessionOperation,
		run func() error,
	) error {
		release, err := runtime.vaultDelivery.acquireDelivery(ctx)
		if err != nil {
			return err
		}
		defer release()
		if err := runtime.vaultLeases.Authorize(ctx, principal, session, operation); err != nil {
			return err
		}
		return run()
	})
	runtime.consoleSessions.SetSessionClosedHook(func(handle console.SessionHandle) {
		_ = owner.SessionClosed(context.Background(), handle)
	})
	return nil
}
