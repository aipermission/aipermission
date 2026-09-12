// Package gatewayworkspace owns encrypted workspace lifecycle, runtime
// construction, and database catalog operations for the gateway.
package gatewayworkspace

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/catalog"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/lifecycle"
	connectorstate "github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/connectors"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/observation"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/security"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtime/storage"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace/runtimeinput"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
	runtimeshutdown "github.com/aipermission/aipermission/backend/internal/workspaceruntime/shutdown"
)

type RuntimeIdentity struct {
	DatabaseID   string
	DatabasePath string
	WorkspaceID  string
	RuntimeID    string
	UIRetryID    string
}

func (identity RuntimeIdentity) Ready() bool {
	return identity.WorkspaceID != "" && identity.RuntimeID != ""
}

// Runtime is an explicit composition DTO. The concrete encrypted runtime is
// retained privately for teardown; consumers receive only owned feature ports.
type Runtime struct {
	Identity    RuntimeIdentity
	Storage     storage.Port
	Connectors  connectorstate.Port
	Security    security.Port
	Observation observation.Port
	owner       *workspaceruntime.Runtime
}

type AdoptInput runtimeinput.Adopt
type OpenInput runtimeinput.Open
type Identity struct {
	ID            string
	Path          string
	RetryIdentity string
}

type PasswordAttempt interface {
	Success()
	Failure()
}

type HTTPDependencies struct {
	BeginAttempt     func(http.ResponseWriter, *http.Request) (PasswordAttempt, bool)
	HasSession       func(*http.Request) bool
	IssueSession     func(http.ResponseWriter) error
	ClearSessions    func(http.ResponseWriter)
	CloseMaintenance func(string)
	Now              func() time.Time
}

type HTTPHandlers interface {
	Status(http.ResponseWriter, *http.Request)
	Setup(http.ResponseWriter, *http.Request)
	Unlock(http.ResponseWriter, *http.Request)
	Lock(http.ResponseWriter, *http.Request)
	Rename(http.ResponseWriter, *http.Request)
	Delete(http.ResponseWriter, *http.Request)
	DeleteLocked(http.ResponseWriter, *http.Request)
	Switch(http.ResponseWriter, *http.Request)
	ChangePassword(http.ResponseWriter, *http.Request)
}

type ActionWorkflow interface {
	BeginShutdown()
	WaitShutdown(context.Context) error
	MarkRunningOutcomeUnknown(context.Context, string) error
}

type CommandWorkflow interface {
	BeginWorkerShutdown()
	WaitWorkers(context.Context) error
	CancelRunning(context.Context, string) error
}

type TransferWorkflow interface {
	BeginShutdown() (bool, error)
	Wait(context.Context) bool
	Recover(context.Context, string, string) error
	Abort()
}

func (runtime *Runtime) WorkspaceIdentity() workspacelifecycle.Identity {
	if runtime == nil {
		return workspacelifecycle.Identity{}
	}
	return workspacelifecycle.Identity{ID: runtime.Identity.DatabaseID, Path: runtime.Identity.DatabasePath, RetryIdentity: runtime.Identity.UIRetryID}
}

func (runtime *Runtime) WorkspaceDatabase() *sql.DB {
	if runtime == nil || runtime.Storage == nil {
		return nil
	}
	return runtime.Storage.DatabaseHandle()
}

func composeRuntime(owner *workspaceruntime.Runtime) (*Runtime, error) {
	if owner == nil || owner.WorkspaceUUID == "" || owner.RuntimeInstanceID == "" {
		return nil, fmt.Errorf("workspace runtime identity is unavailable")
	}
	return &Runtime{
		Identity: RuntimeIdentity{
			DatabaseID: owner.ID, DatabasePath: owner.Path,
			WorkspaceID: owner.WorkspaceUUID, RuntimeID: owner.RuntimeInstanceID,
			UIRetryID: owner.UIRetryIdentity,
		},
		Storage: &owner.Storage, Connectors: &owner.Connectors, Security: &owner.Security, Observation: &owner.Observation, owner: owner,
	}, nil
}

func (runtime *Runtime) ConfiguredGatewaySecret() string {
	if runtime == nil || runtime.owner == nil {
		return ""
	}
	return runtime.owner.GatewaySecret
}

func (runtime *Runtime) TagActionIdentity(canonical []byte) (string, error) {
	if runtime == nil || runtime.owner == nil {
		return "", fmt.Errorf("workspace action identity is unavailable")
	}
	return workspaceruntime.TagActionIdentity(runtime.owner, canonical)
}

func (runtime *Runtime) WaitTeardown(ctx context.Context) error {
	if runtime == nil || runtime.owner == nil {
		return nil
	}
	return runtime.owner.WaitTeardown(ctx)
}

type Dependencies struct {
	DataPath              string
	Open                  func(string, string, string) (*Runtime, error)
	Close                 func(*Runtime) error
	OnActivated, OnOpened func(*Runtime)
	Move                  func(string, string) error
	Delete                func(string) error
	Publish               func(string, string) error
	GatewaySecret         func() string
}

type LifecyclePort interface {
	AcquireReadContext(context.Context) (func(), error)
	AcquireMutationContext(context.Context) (func(), error)
	Import(context.Context, workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error)
	CloseAll(context.Context) error
}

// Component is the single owner of workspace registry and lifecycle state.
// Runtime construction and catalog mutations also enter through this boundary.
type Component struct {
	lifecycle *lifecycle.Component
}

func NewComponent(path string, describe func(*Runtime) Identity) *Component {
	catalog.Scavenge(path, time.Now())
	if describe == nil {
		describe = func(runtime *Runtime) Identity {
			if runtime == nil {
				return Identity{}
			}
			identity := runtime.WorkspaceIdentity()
			return Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
		}
	}
	return &Component{lifecycle: lifecycle.NewComponent(path, catalog.DefaultID(path), func(runtime lifecycle.Runtime) workspacelifecycle.Identity {
		owned, _ := runtime.(*Runtime)
		identity := describe(owned)
		return workspacelifecycle.Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
	})}
}

func (component *Component) Configure(dependencies Dependencies) error {
	if component == nil || component.lifecycle == nil {
		return InitializationError()
	}
	var open func(string, string, string) (lifecycle.Runtime, error)
	if dependencies.Open != nil {
		open = func(path, id, password string) (lifecycle.Runtime, error) {
			return dependencies.Open(path, id, password)
		}
	}
	var closeRuntime func(lifecycle.Runtime) error
	if dependencies.Close != nil {
		closeRuntime = func(runtime lifecycle.Runtime) error {
			owned, ok := runtime.(*Runtime)
			if !ok {
				return InitializationError()
			}
			return dependencies.Close(owned)
		}
	}
	var onActivated, onOpened func(lifecycle.Runtime)
	if dependencies.OnActivated != nil {
		onActivated = func(runtime lifecycle.Runtime) {
			if owned, ok := runtime.(*Runtime); ok {
				dependencies.OnActivated(owned)
			}
		}
	}
	if dependencies.OnOpened != nil {
		onOpened = func(runtime lifecycle.Runtime) {
			if owned, ok := runtime.(*Runtime); ok {
				dependencies.OnOpened(owned)
			}
		}
	}
	return component.lifecycle.Configure(lifecycle.Dependencies{
		DataPath: dependencies.DataPath, Open: open, Close: closeRuntime,
		OnActivated: onActivated, OnOpened: onOpened,
		Move: dependencies.Move, Delete: dependencies.Delete,
		Publish: dependencies.Publish, GatewaySecret: dependencies.GatewaySecret,
	})
}

func (component *Component) IsUnlocked() bool {
	return component != nil && component.lifecycle != nil && component.lifecycle.IsUnlocked()
}
func (component *Component) Selection() Identity {
	if component == nil || component.lifecycle == nil {
		return Identity{}
	}
	identity := component.lifecycle.Selection()
	return Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
}
func (component *Component) Lookup(id string) (*Runtime, bool) {
	if component == nil || component.lifecycle == nil {
		return nil, false
	}
	runtime, ok := component.lifecycle.Lookup(id)
	owned, ownedOK := runtime.(*Runtime)
	return owned, ok && ownedOK
}
func (component *Component) Activate(runtime *Runtime) {
	if component != nil && component.lifecycle != nil {
		component.lifecycle.Activate(runtime)
	}
}
func (component *Component) Active() *Runtime {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	runtime := component.lifecycle.Active()
	owned, _ := runtime.(*Runtime)
	return owned
}
func (component *Component) Snapshot() []*Runtime {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	items := component.lifecycle.Snapshot()
	runtimes := make([]*Runtime, 0, len(items))
	for _, runtime := range items {
		if owned, ok := runtime.(*Runtime); ok {
			runtimes = append(runtimes, owned)
		}
	}
	return runtimes
}
func (component *Component) Len() int {
	if component == nil || component.lifecycle == nil {
		return 0
	}
	return component.lifecycle.Len()
}
func (component *Component) DatabaseName() (string, error) {
	if component == nil || component.lifecycle == nil {
		return "", InitializationError()
	}
	return component.lifecycle.DatabaseName()
}
func (component *Component) AcquireReadContext(ctx context.Context) (func(), error) {
	if component == nil || component.lifecycle == nil {
		return func() {}, nil
	}
	return component.lifecycle.AcquireReadContext(ctx)
}
func (component *Component) AcquireMutationContext(ctx context.Context) (func(), error) {
	if component == nil || component.lifecycle == nil {
		return func() {}, nil
	}
	return component.lifecycle.AcquireMutationContext(ctx)
}
func (component *Component) Import(ctx context.Context, input workspacelifecycle.ImportInput) (workspacelifecycle.Transition, error) {
	if component == nil || component.lifecycle == nil {
		return workspacelifecycle.Transition{}, InitializationError()
	}
	return component.lifecycle.Import(ctx, input)
}
func (component *Component) CloseAll(ctx context.Context) error {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	return component.lifecycle.CloseAll(ctx)
}
func (component *Component) HTTP(dependencies HTTPDependencies) HTTPHandlers {
	if component == nil || component.lifecycle == nil {
		return nil
	}
	converted := lifecycle.HTTPDependencies{
		HasSession: dependencies.HasSession, IssueSession: dependencies.IssueSession,
		ClearSessions: dependencies.ClearSessions, CloseMaintenance: dependencies.CloseMaintenance, Now: dependencies.Now,
	}
	if dependencies.BeginAttempt != nil {
		converted.BeginAttempt = func(w http.ResponseWriter, r *http.Request) (lifecycle.PasswordAttempt, bool) {
			return dependencies.BeginAttempt(w, r)
		}
	}
	return component.lifecycle.HTTP(converted)
}

func (component *Component) Adopt(ctx context.Context, input AdoptInput) (*Runtime, error) {
	state, err := foundation.Adopt(ctx, foundation.AdoptInput{
		ID: input.ID, Path: input.Path, Database: input.Database, Vault: input.Vault,
		TokenStore: input.TokenStore, ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Registry: input.Registry, AdapterRegistry: input.AdapterRegistry, RuntimeInstanceID: input.RuntimeInstanceID,
	})
	if err != nil {
		return nil, err
	}
	return composeRuntime(workspaceruntime.New(state))
}
func (component *Component) Open(ctx context.Context, input OpenInput) (*Runtime, error) {
	state, err := foundation.Open(ctx, foundation.OpenInput{
		ID: input.ID, Path: input.Path, Password: input.Password,
		ConfiguredGatewaySecret: input.ConfiguredGatewaySecret,
		Registry:                input.Registry, AdapterRegistry: input.AdapterRegistry,
	})
	if err != nil {
		return nil, err
	}
	return composeRuntime(workspaceruntime.New(state))
}
func (component *Component) Discard(runtime *Runtime, resolveTransfers func() TransferWorkflow, onComplete func()) error {
	if runtime == nil {
		return nil
	}
	var resolver runtimeshutdown.TransferWorkflowResolver
	if resolveTransfers != nil {
		resolver = func() runtimeshutdown.TransferWorkflow { return resolveTransfers() }
	}
	err := runtimeshutdown.Discard(runtime.owner, resolver, onComplete)
	return err
}
func (component *Component) Close(runtime *Runtime, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error), resolveTransfers func() TransferWorkflow, onComplete func()) error {
	if runtime == nil {
		return nil
	}
	var actions runtimeshutdown.ActionWorkflowResolver
	if resolveActions != nil {
		actions = func() (runtimeshutdown.ActionWorkflow, error) { return resolveActions() }
	}
	var commands runtimeshutdown.CommandWorkflowResolver
	if resolveCommands != nil {
		commands = func() (runtimeshutdown.CommandWorkflow, error) { return resolveCommands() }
	}
	var transfers runtimeshutdown.TransferWorkflowResolver
	if resolveTransfers != nil {
		transfers = func() runtimeshutdown.TransferWorkflow { return resolveTransfers() }
	}
	err := runtimeshutdown.Close(runtime.owner, actions, commands, transfers, onComplete)
	return err
}
func (component *Component) Move(currentPath, targetPath string) error {
	return catalog.Move(currentPath, targetPath)
}
func (component *Component) Delete(path string) error { return catalog.Delete(path) }
func (component *Component) Publish(sourcePath, targetPath string) error {
	return catalog.Publish(sourcePath, targetPath)
}
func (component *Component) LooksPlaintext(path string) bool { return catalog.LooksPlaintext(path) }
func InitializationError() error                             { return lifecycle.InitializationError() }

var _ LifecyclePort = (*Component)(nil)
