package api

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type projectVaultDeliveryGate struct{ runtime *databaseRuntime }

func (g projectVaultDeliveryGate) AcquireDelivery(ctx context.Context) (func(), error) {
	if g.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return g.runtime.vaultDelivery.acquireDelivery(ctx)
}

func (g projectVaultDeliveryGate) AcquireExclusive(ctx context.Context) (func(), error) {
	if g.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return g.runtime.vaultDelivery.acquireExclusive(ctx)
}

type projectVaultMutationPort struct {
	server  *Server
	runtime *databaseRuntime
}

func (p projectVaultMutationPort) WithMutation(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if p.server == nil || p.runtime == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	return p.server.withAuditedMutation(ctx, p.runtime, "user", nil, 0, action, payload, mutate)
}

func (p projectVaultMutationPort) Observe(ctx context.Context, action string, payload any) error {
	if p.server == nil || p.runtime == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	return p.server.writeAuditRequired(ctx, p.runtime, "user", nil, 0, action, payload)
}

func (s *Server) projectVaultRuntime(runtime *databaseRuntime) (*projectvault.Runtime, error) {
	if s == nil || runtime == nil || runtime.database == nil || runtime.vault == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	runtime.projectVaultMu.Lock()
	defer runtime.projectVaultMu.Unlock()
	if runtime.projectVault != nil {
		return runtime.projectVault, nil
	}
	store, err := projectvault.NewStore(runtime.database, runtime.vault, runtime.workspaceUUID)
	if err != nil {
		return nil, err
	}
	owner, err := projectvault.NewRuntime(projectvault.RuntimeDependencies{
		Store:     store,
		Delivery:  projectVaultDeliveryGate{runtime: runtime},
		Mutations: projectVaultMutationPort{server: s, runtime: runtime},
		InvalidateSessions: func(ctx context.Context, sessions []projectvault.SessionReference, scope projectvault.SessionMutationScope) error {
			return s.invalidateVaultMutationAfterCommit(ctx, runtime, sessions, scope)
		},
		AllowGenerate: func(key string) bool {
			return s.vaultGenerateLimiter != nil && s.vaultGenerateLimiter.Allow(key)
		},
		AllowReveal: func(key string) bool {
			return s.vaultRevealLimiter != nil && s.vaultRevealLimiter.Allow(key)
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Project Vault runtime: %w", err)
	}
	runtime.projectVault = owner
	return owner, nil
}
