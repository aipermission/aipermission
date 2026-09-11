// Package gatewayworkspace owns encrypted workspace lifecycle, runtime
// construction, and database catalog operations for the gateway.
package gatewayworkspace

import (
	"context"
	"database/sql"
	"time"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/catalog"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/lifecycle"
	runtimefactory "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimecontract"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimeinput"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Runtime = runtimecontract.Runtime
type Vault = vault.Vault
type TokenStore = tokens.Store
type AdoptInput = runtimeinput.Adopt
type OpenInput = runtimeinput.Open
type Identity = lifecycle.Identity
type Dependencies = lifecycle.Dependencies
type HTTPDependencies = lifecycle.HTTPDependencies
type HTTPHandlers = lifecycle.HTTPHandlers
type PasswordAttempt = lifecycle.PasswordAttempt
type ActionWorkflow = runtimefactory.ActionWorkflow
type CommandWorkflow = runtimefactory.CommandWorkflow

type LifecyclePort interface {
	AcquireRead() func()
	AcquireMutation() func()
	Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error)
	CloseAll() error
}

// Component is the single owner of workspace registry and lifecycle state.
// Runtime construction and catalog mutations also enter through this boundary.
type Component struct {
	lifecycle *lifecycle.Component
}

func NewComponent(path string, describe func(Runtime) Identity) *Component {
	catalog.Scavenge(path, time.Now())
	if describe == nil {
		describe = func(runtime Runtime) Identity {
			if runtime == nil {
				return Identity{}
			}
			return runtime.WorkspaceIdentity()
		}
	}
	return &Component{lifecycle: lifecycle.NewComponent(path, catalog.DefaultID(path), describe)}
}

func (component *Component) Configure(dependencies Dependencies) error {
	if component == nil || component.lifecycle == nil {
		return ErrInitialization
	}
	return component.lifecycle.Configure(dependencies)
}

func (component *Component) IsUnlocked() bool {
	return component != nil && component.lifecycle != nil && component.lifecycle.IsUnlocked()
}
func (component *Component) Selection() Identity {
	if component == nil || component.lifecycle == nil {
		return Identity{}
	}
	return component.lifecycle.Selection()
}
func (component *Component) Lookup(id string) (Runtime, bool) {
	if component == nil || component.lifecycle == nil {
		return nil, false
	}
	return component.lifecycle.Lookup(id)
}
func (component *Component) Activate(runtime Runtime) {
	if component != nil && component.lifecycle != nil {
		component.lifecycle.Activate(runtime)
	}
}
func (component *Component) Active() Runtime {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	return component.lifecycle.Active()
}
func (component *Component) Snapshot() []Runtime {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	return component.lifecycle.Snapshot()
}
func (component *Component) Len() int {
	if component == nil || component.lifecycle == nil {
		return 0
	}
	return component.lifecycle.Len()
}
func (component *Component) DatabaseName() (string, error) {
	if component == nil || component.lifecycle == nil {
		return "", ErrInitialization
	}
	return component.lifecycle.DatabaseName()
}
func (component *Component) AcquireRead() func() {
	if component == nil || component.lifecycle == nil {
		return func() {}
	}
	return component.lifecycle.AcquireRead()
}
func (component *Component) AcquireMutation() func() {
	if component == nil || component.lifecycle == nil {
		return func() {}
	}
	return component.lifecycle.AcquireMutation()
}
func (component *Component) Import(ctx context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
	if component == nil || component.lifecycle == nil {
		return workspacelifecycle.Transition{}, ErrInitialization
	}
	return component.lifecycle.Import(ctx, input)
}
func (component *Component) CloseAll() error {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	return component.lifecycle.CloseAll()
}
func (component *Component) HTTP(dependencies HTTPDependencies) HTTPHandlers {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	return component.lifecycle.HTTP(dependencies)
}

func (component *Component) Adopt(ctx context.Context, input AdoptInput) (Runtime, error) {
	return runtimefactory.Adopt(ctx, input)
}
func (component *Component) Open(ctx context.Context, input OpenInput) (Runtime, error) {
	return runtimefactory.Open(ctx, input)
}
func (component *Component) Discard(runtime Runtime) error { return runtimefactory.Discard(runtime) }
func (component *Component) Close(runtime Runtime, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error)) error {
	return runtimefactory.Close(runtime, resolveActions, resolveCommands)
}
func (component *Component) Move(currentPath, targetPath string) error {
	return catalog.Move(currentPath, targetPath)
}
func (component *Component) Delete(path string) error { return catalog.Delete(path) }
func (component *Component) Publish(sourcePath, targetPath string) error {
	return catalog.Publish(sourcePath, targetPath)
}
func (component *Component) LooksPlaintext(path string) bool { return catalog.LooksPlaintext(path) }
func (component *Component) HasActiveRemoteBackup(ctx context.Context, database *sql.DB) (bool, error) {
	return lifecycle.HasActiveRemoteBackup(ctx, database)
}

func (component *Component) ValidateRemoteBackupPassword(password, databaseName string) error {
	return lifecycle.ValidateRemoteBackupPassword(password, databaseName)
}

func (component *Component) PasswordPolicyError(err error) error {
	return lifecycle.PasswordPolicyError(err)
}

var (
	ErrInitialization = lifecycle.ErrInitialization
)

var _ LifecyclePort = (*Component)(nil)
