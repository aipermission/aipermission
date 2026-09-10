package api

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/applicationvault"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

func (s *Server) vaultApplication() *applicationvault.Component {
	return applicationvault.New(applicationvault.ProjectDependencies{
		InvalidateSessions: func(ctx context.Context, runtime *workspaceruntime.Runtime, sessions []projectvault.SessionReference, scope projectvault.SessionMutationScope) error {
			return s.invalidateVaultMutationAfterCommit(ctx, runtime, sessions, scope)
		},
		LiveConsoleKind: func(kind string) (string, bool) {
			adapter := s.connectorLiveConsoleTargetAdapterFor(kind)
			if adapter == nil {
				return "", false
			}
			return adapter.LiveConsoleCapabilityKind(), true
		},
		SessionEnvironment: func(ctx context.Context, runtime *workspaceruntime.Runtime, runtimeID int64) (bool, error) {
			err := requireSessionEnvironmentCapability(ctx, s, runtime, runtimeID)
			if errors.Is(err, connectors.ErrSessionEnvironmentUnsupported) {
				return false, nil
			}
			return err == nil, err
		},
		Mutate: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload func() any, mutate func(*sql.Tx) error) error {
			return s.withAuditedMutation(ctx, runtime, "user", nil, 0, action, payload, mutate)
		},
		Observe: func(ctx context.Context, runtime *workspaceruntime.Runtime, action string, payload any) error {
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

func (s *Server) projectVaultRuntime(runtime *databaseRuntime) (*projectvault.Runtime, error) {
	return s.vaultApplication().ProjectRuntime(runtime)
}

func (s *Server) projectVaultHTTPScope(w http.ResponseWriter) (projectvault.HTTPScope, bool) {
	runtime, ok := s.activeRuntimeOrLocked(w)
	if !ok {
		return projectvault.HTTPScope{}, false
	}
	owner, err := s.projectVaultRuntime(runtime)
	if err != nil {
		writeInternalError(w)
		return projectvault.HTTPScope{}, false
	}
	return projectvault.HTTPScope{
		Runtime: owner, RuntimeID: runtime.ID,
		SessionCatalog: s.vaultApplication().SessionCatalog(runtime),
	}, true
}
