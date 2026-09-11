package gatewayvault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
)

var projectRuntimeStateKey = componentstate.NewKey[*projectRuntimeHandle]("project-vault-runtime")

type Dependencies struct {
	LiveConsoleKind func(string) (string, bool)
	AllowGenerate   func(string) bool
	AllowReveal     func(string) bool
	AllowRequest    func(string) bool
}

type Component struct {
	dependencies Dependencies
}

func New(dependencies Dependencies) *Component { return &Component{dependencies: dependencies} }

type ProjectVaultApplication interface {
	List(context.Context, projectvault.ListFilter) ([]projectvault.Item, int, error)
	Get(context.Context, int64) (projectvault.Item, error)
	Create(context.Context, projectvault.CreateInput) (projectvault.Item, error)
	UpdateMetadata(context.Context, projectvault.UpdateMetadataInput) (projectvault.Item, error)
	ReplaceValue(context.Context, projectvault.ReplaceRuntimeValueInput) (projectvault.Item, error)
	GeneratePreview(context.Context, int64, string, string) (projectvault.GeneratedPreviewResult, error)
	Reveal(context.Context, int64, string) (string, error)
	Delete(context.Context, int64, int64, int64) error
	ListDefaultBindings(context.Context, projectvault.DefaultBindingFilter) ([]projectvault.DefaultBinding, error)
	SaveDefaultBinding(context.Context, projectvault.DefaultBindingInput) (projectvault.DefaultBinding, error)
	DeleteDefaultBinding(context.Context, int64, int64) error
}

type projectRuntimeHandle struct{ runtime *projectvault.Runtime }

type deliveryGate struct{ runtime Runtime }

func (gate deliveryGate) AcquireDelivery(ctx context.Context) (func(), error) {
	if gate.runtime.Session.AcquireDelivery == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return gate.runtime.Session.AcquireDelivery(ctx)
}

func (gate deliveryGate) AcquireExclusive(ctx context.Context) (func(), error) {
	if gate.runtime.Session.AcquireExclusive == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return gate.runtime.Session.AcquireExclusive(ctx)
}

type mutationPort struct {
	component *Component
	runtime   Runtime
}

func (port mutationPort) WithMutation(ctx context.Context, action string, payload func() any, mutate func(*sql.Tx) error) error {
	if port.component == nil || port.runtime.Project.Mutate == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	return port.runtime.Project.Mutate(ctx, action, payload, mutate)
}

func (port mutationPort) Observe(ctx context.Context, action string, payload any) error {
	if port.component == nil || port.runtime.Project.Observe == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	return port.runtime.Project.Observe(ctx, action, payload)
}

func (component *Component) ProjectRuntime(runtime Runtime) (ProjectVaultApplication, error) {
	if component == nil || runtime.Storage.Database == nil || runtime.Storage.SecretVault == nil || runtime.Project.State == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	handle, err := componentstate.LoadOrCreate(runtime.Project.State, projectRuntimeStateKey, func() (*projectRuntimeHandle, error) {
		store, err := projectvault.NewStore(runtime.Storage.Database, runtime.Storage.SecretVault, runtime.Storage.WorkspaceID)
		if err != nil {
			return nil, err
		}
		owner, err := projectvault.NewRuntime(projectvault.RuntimeDependencies{
			Store: store, Delivery: deliveryGate{runtime: runtime}, Mutations: mutationPort{component: component, runtime: runtime},
			InvalidateSessions: runtime.Project.InvalidateSessions,
			BindingTargets:     bindingTargets{component: component, runtime: runtime},
			AllowGenerate:      component.dependencies.AllowGenerate, AllowReveal: component.dependencies.AllowReveal,
		})
		if err != nil {
			return nil, fmt.Errorf("initialize Project Vault runtime: %w", err)
		}
		return &projectRuntimeHandle{runtime: owner}, nil
	})
	if err != nil {
		return nil, err
	}
	if handle == nil || handle.runtime == nil {
		return nil, projectvault.ErrRuntimeUnavailable
	}
	return handle.runtime, nil
}

type bindingTargets struct {
	component *Component
	runtime   Runtime
}

func (port bindingTargets) ValidateDefaultBindingTarget(ctx context.Context, targetID, profileID int64) error {
	if port.component == nil || port.runtime.Storage.Database == nil {
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
	capabilityKind, ok := port.component.dependencies.LiveConsoleKind(target.ConnectorKind)
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
	if port.runtime.Project.SessionEnvironment == nil {
		return projectvault.ErrRuntimeUnavailable
	}
	supported, err := port.runtime.Project.SessionEnvironment(ctx, surface.ID)
	if err != nil || !supported {
		return projectvault.ErrSessionEnvironmentUnsupported
	}
	return nil
}

type SessionCatalog struct {
	component *Component
	runtime   Runtime
}

func (component *Component) SessionCatalog(runtime Runtime) SessionCatalog {
	return SessionCatalog{component: component, runtime: runtime}
}

func (catalog SessionCatalog) ResolveSessionOptionsTarget(ctx context.Context, runtimeID int64) (projectvault.SessionOptionsTarget, error) {
	if catalog.component == nil || catalog.runtime.Storage.Database == nil {
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
	if catalog.runtime.Project.SessionEnvironment == nil {
		return projectvault.SessionOptionsTarget{}, projectvault.ErrRuntimeUnavailable
	}
	supported, err := catalog.runtime.Project.SessionEnvironment(ctx, runtimeID)
	if err != nil {
		return projectvault.SessionOptionsTarget{}, err
	}
	return projectvault.SessionOptionsTarget{
		ProjectID: target.ProjectID, TargetID: surface.TargetID, ProfileID: surface.ProfileID,
		SessionEnvironmentSupported: supported,
	}, nil
}

func (catalog SessionCatalog) ListSessionOptionsProjects(ctx context.Context) ([]projectvault.SessionOptionsProject, error) {
	if catalog.runtime.Storage.Database == nil {
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
