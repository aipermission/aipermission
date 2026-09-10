// Package applicationvault composes Project Vault and agent Vault operations
// without exposing encrypted workspace state to HTTP or MCP transports.
package applicationvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type ProjectDependencies struct {
	InvalidateSessions func(context.Context, *workspaceruntime.Runtime, []projectvault.SessionReference, projectvault.SessionMutationScope) error
	LiveConsoleKind    func(string) (string, bool)
	SessionEnvironment func(context.Context, *workspaceruntime.Runtime, int64) (bool, error)
	Mutate             func(context.Context, *workspaceruntime.Runtime, string, func() any, func(*sql.Tx) error) error
	Observe            func(context.Context, *workspaceruntime.Runtime, string, any) error
	AllowGenerate      func(string) bool
	AllowReveal        func(string) bool
}

type Component struct {
	projects ProjectDependencies
	actions  ActionDependencies
	requests RequestDependencies
}

func New(projects ProjectDependencies) *Component { return &Component{projects: projects} }

type deliveryGate struct{ runtime *workspaceruntime.Runtime }

func (gate deliveryGate) AcquireDelivery(ctx context.Context) (func(), error) {
	if gate.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return gate.runtime.Security.VaultDelivery.AcquireDelivery(ctx)
}

func (gate deliveryGate) AcquireExclusive(ctx context.Context) (func(), error) {
	if gate.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return gate.runtime.Security.VaultDelivery.AcquireExclusive(ctx)
}

type mutationPort struct {
	component *Component
	runtime   *workspaceruntime.Runtime
}

func (port mutationPort) WithMutation(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime == nil || port.component.projects.Mutate == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	return port.component.projects.Mutate(ctx, port.runtime, action, payload, mutate)
}

func (port mutationPort) Observe(ctx context.Context, action string, payload any) error {
	if port.component == nil || port.runtime == nil || port.component.projects.Observe == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	return port.component.projects.Observe(ctx, port.runtime, action, payload)
}

func (component *Component) ProjectRuntime(runtime *workspaceruntime.Runtime) (*projectvault.Runtime, error) {
	if component == nil || runtime == nil || runtime.Storage.Database == nil || runtime.Storage.Vault == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return runtime.Operations.ProjectVaultOrCreate(func() (*projectvault.Runtime, error) {
		store, err := projectvault.NewStore(runtime.Storage.Database, runtime.Storage.Vault, runtime.WorkspaceUUID)
		if err != nil {
			return nil, err
		}
		owner, err := projectvault.NewRuntime(projectvault.RuntimeDependencies{
			Store: store, Delivery: deliveryGate{runtime: runtime}, Mutations: mutationPort{component: component, runtime: runtime},
			InvalidateSessions: func(ctx context.Context, sessions []projectvault.SessionReference, scope projectvault.SessionMutationScope) error {
				return component.projects.InvalidateSessions(ctx, runtime, sessions, scope)
			},
			BindingTargets: bindingTargets{component: component, runtime: runtime},
			AllowGenerate:  component.projects.AllowGenerate, AllowReveal: component.projects.AllowReveal,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize Project Vault runtime: %w", err)
		}
		return owner, nil
	})
}

type bindingTargets struct {
	component *Component
	runtime   *workspaceruntime.Runtime
}

func (port bindingTargets) ValidateDefaultBindingTarget(ctx context.Context, targetID, profileID int64) error {
	if port.component == nil || port.runtime == nil || port.runtime.Storage.Database == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	store := connectortargets.NewStore(port.runtime.Storage.Database)
	target, err := store.GetTarget(ctx, targetID)
	if errors.Is(err, connectortargets.ErrTargetNotFound) {
		return projectvault.ErrBindingTargetNotFound
	}
	if err != nil {
		return err
	}
	capabilityKind, ok := port.component.projects.LiveConsoleKind(target.ConnectorKind)
	if !ok {
		return projectvault.ErrSessionEnvironmentUnsupported
	}
	surface, err := store.GetRuntimeSurfaceByProfile(ctx, target.ConnectorKind, targetID, profileID, capabilityKind)
	if errors.Is(err, connectortargets.ErrTargetProfileNotFound) {
		return projectvault.ErrBindingTargetNotFound
	}
	if err != nil {
		return err
	}
	supported, err := port.component.projects.SessionEnvironment(ctx, port.runtime, surface.ID)
	if err != nil || !supported {
		return projectvault.ErrSessionEnvironmentUnsupported
	}
	return nil
}

type SessionCatalog struct {
	component *Component
	runtime   *workspaceruntime.Runtime
}

func (component *Component) SessionCatalog(runtime *workspaceruntime.Runtime) SessionCatalog {
	return SessionCatalog{component: component, runtime: runtime}
}

func (catalog SessionCatalog) ResolveSessionOptionsTarget(ctx context.Context, runtimeID int64) (projectvault.SessionOptionsTarget, error) {
	if catalog.component == nil || catalog.runtime == nil || catalog.runtime.Storage.Database == nil {
		return projectvault.SessionOptionsTarget{}, projectvault.ErrRuntimeUnavailable
	}
	store := connectortargets.NewStore(catalog.runtime.Storage.Database)
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
	supported, err := catalog.component.projects.SessionEnvironment(ctx, catalog.runtime, runtimeID)
	if err != nil {
		return projectvault.SessionOptionsTarget{}, err
	}
	return projectvault.SessionOptionsTarget{
		ProjectID: target.ProjectID, TargetID: surface.TargetID, ProfileID: surface.ProfileID,
		SessionEnvironmentSupported: supported,
	}, nil
}

func (catalog SessionCatalog) ListSessionOptionsProjects(ctx context.Context) ([]projectvault.SessionOptionsProject, error) {
	if catalog.runtime == nil || catalog.runtime.Storage.Database == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	items, err := projectstore.NewStore(catalog.runtime.Storage.Database).List(ctx)
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
