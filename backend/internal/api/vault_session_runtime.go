package api

import (
	"context"

	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
)

func (s *Server) configureVaultSessionRuntime(runtime *databaseRuntime) {
	if runtime == nil || runtime.consoleSessions == nil || runtime.vaultLeases == nil {
		return
	}
	runtime.consoleSessions.SetAuthorizer(func(
		ctx context.Context,
		principal executionprincipal.Principal,
		session console.SessionAuthorization,
		operation console.SessionOperation,
		run func() error,
	) error {
		release, err := runtime.vaultDelivery.acquire(ctx)
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
		runtime.vaultLeases.RevokeSession(handle)
		_ = revokePersistedVaultLease(context.Background(), runtime, handle.ID, handle.Generation)
	})
}
