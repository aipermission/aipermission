package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func (s *Server) vaultApplication() *gatewayvault.Application {
	return gatewayvault.NewApplication(gatewayvault.ProjectDependencies{
		InvalidateSessions: func(ctx context.Context, runtime gatewayinfra.Runtime, sessions []gatewayvault.SessionReference, scope gatewayvault.SessionMutationScope) error {
			return s.invalidateVaultMutationAfterCommit(ctx, runtime, sessions, scope)
		},
		LiveConsoleKind: func(kind string) (string, bool) {
			adapter := s.connectorLiveConsoleTargetAdapterFor(kind)
			if adapter == nil {
				return "", false
			}
			return adapter.LiveConsoleCapabilityKind(), true
		},
		SessionEnvironment: func(ctx context.Context, runtime gatewayinfra.Runtime, runtimeID int64) (bool, error) {
			err := requireSessionEnvironmentCapability(ctx, s, runtime, runtimeID)
			if errors.Is(err, connectors.ErrSessionEnvironmentUnsupported) {
				return false, nil
			}
			return err == nil, err
		},
		Mutate: func(ctx context.Context, runtime gatewayinfra.Runtime, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		Observe: func(ctx context.Context, runtime gatewayinfra.Runtime, action string, payload any) error {
			return s.writeAuditRequired(ctx, runtime, "user", nil, 0, action, payload)
		},
		AllowGenerate: func(key string) bool {
			return s.controlState.VaultGenerateLimiter != nil && s.controlState.VaultGenerateLimiter.Allow(key)
		},
		AllowReveal: func(key string) bool {
			return s.controlState.VaultRevealLimiter != nil && s.controlState.VaultRevealLimiter.Allow(key)
		},
	})
}

func (s *Server) projectVaultRuntime(runtime databaseRuntime) (*gatewayvault.ProjectVaultRuntime, error) {
	return s.vaultApplication().ProjectRuntime(runtime)
}

func (s *Server) projectVaultHTTPScope(w http.ResponseWriter) (gatewayvault.ProjectVaultHTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return gatewayvault.ProjectVaultHTTPScope{}, false
	}
	owner, err := s.projectVaultRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return gatewayvault.ProjectVaultHTTPScope{}, false
	}
	return gatewayvault.ProjectVaultHTTPScope{
		Runtime: owner, RuntimeID: runtime.DatabaseIdentifier(),
		SessionCatalog: s.vaultApplication().SessionCatalog(runtime),
	}, true
}
