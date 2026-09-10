package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

type projectVaultDeliveryGate struct{ runtime *databaseRuntime }

func (g projectVaultDeliveryGate) AcquireDelivery(ctx context.Context) (func(), error) {
	if g.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return g.runtime.vaultDelivery.AcquireDelivery(ctx)
}

func (g projectVaultDeliveryGate) AcquireExclusive(ctx context.Context) (func(), error) {
	if g.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return g.runtime.vaultDelivery.AcquireExclusive(ctx)
}

type projectVaultMutationPort struct {
	server  *Server
	runtime *databaseRuntime
}

type projectVaultBindingTargets struct {
	server  *Server
	runtime *databaseRuntime
}

type projectVaultSessionOptionsCatalog struct {
	server  *Server
	runtime *databaseRuntime
}

func (p projectVaultBindingTargets) ValidateDefaultBindingTarget(ctx context.Context, targetID, profileID int64) error {
	if p.server == nil || p.runtime == nil || p.runtime.database == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	store := connectortargets.NewStore(p.runtime.database)
	target, err := store.GetTarget(ctx, targetID)
	if errors.Is(err, connectortargets.ErrTargetNotFound) {
		return projectvault.ErrBindingTargetNotFound
	}
	if err != nil {
		return err
	}
	adapter := p.server.connectorLiveConsoleTargetAdapterFor(target.ConnectorKind)
	if adapter == nil {
		return projectvault.ErrSessionEnvironmentUnsupported
	}
	surface, err := store.GetRuntimeSurfaceByProfile(
		ctx, target.ConnectorKind, targetID, profileID, adapter.LiveConsoleCapabilityKind(),
	)
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
		return projectvault.ErrBindingTargetNotFound
	}
	if err != nil {
		return err
	}
	if err := requireSessionEnvironmentCapability(ctx, p.server, p.runtime, surface.ID); err != nil {
		return projectvault.ErrSessionEnvironmentUnsupported
	}
	return nil
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
		BindingTargets: projectVaultBindingTargets{server: s, runtime: runtime},
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
		Runtime: owner, RuntimeID: runtime.id,
		SessionCatalog: projectVaultSessionOptionsCatalog{server: s, runtime: runtime},
	}, true
}

func (c projectVaultSessionOptionsCatalog) ResolveSessionOptionsTarget(
	ctx context.Context, runtimeID int64,
) (projectvault.SessionOptionsTarget, error) {
	if c.server == nil || c.runtime == nil || c.runtime.database == nil {
		return projectvault.SessionOptionsTarget{}, projectvault.ErrRuntimeUnavailable
	}
	store := connectortargets.NewStore(c.runtime.database)
	surface, err := store.GetRuntimeSurface(ctx, runtimeID)
	if errors.Is(err, connectortargets.ErrRuntimeSurfaceNotFound) {
		return projectvault.SessionOptionsTarget{}, projectvault.ErrSessionRuntimeNotFound
	}
	if err != nil {
		return projectvault.SessionOptionsTarget{}, err
	}
	target, err := store.GetTarget(ctx, surface.TargetID)
	if errors.Is(err, connectortargets.ErrTargetNotFound) {
		return projectvault.SessionOptionsTarget{}, projectvault.ErrSessionTargetNotFound
	}
	if err != nil {
		return projectvault.SessionOptionsTarget{}, err
	}
	supported := true
	if err := requireSessionEnvironmentCapability(ctx, c.server, c.runtime, runtimeID); err != nil {
		if !errors.Is(err, connectors.ErrSessionEnvironmentUnsupported) {
			return projectvault.SessionOptionsTarget{}, err
		}
		supported = false
	}
	return projectvault.SessionOptionsTarget{
		ProjectID: target.ProjectID, TargetID: surface.TargetID, ProfileID: surface.ProfileID,
		SessionEnvironmentSupported: supported,
	}, nil
}

func (c projectVaultSessionOptionsCatalog) ListSessionOptionsProjects(
	ctx context.Context,
) ([]projectvault.SessionOptionsProject, error) {
	if c.runtime == nil || c.runtime.database == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	items, err := projectstore.NewStore(c.runtime.database).List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]projectvault.SessionOptionsProject, 0, len(items))
	for _, item := range items {
		result = append(result, projectvault.SessionOptionsProject{
			ID: item.ID, Name: item.Name, Slug: item.Slug, TargetCount: item.TargetCount,
			CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
		})
	}
	return result, nil
}
