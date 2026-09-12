package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"sync"

	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

// Component owns process-scoped gateway infrastructure. Callers compose
// behavior through its methods instead of sharing mutable state containers.
type Component struct {
	backupOperations backupOperationLimiter
	identity         *componentIdentity
	observation      *observationapp.Component
	workspace        *gatewayworkspace.Component
	runtimeMu        sync.Mutex
	handlesByOwner   map[*gatewayworkspace.Runtime]*WorkspaceHandle
}

func NewComponent(dataPath string, describe func(*WorkspaceHandle) Identity) *Component {
	if describe == nil {
		describe = func(handle *WorkspaceHandle) Identity {
			if handle == nil {
				return Identity{}
			}
			return handle.WorkspaceIdentity()
		}
	}
	component := &Component{
		handlesByOwner: make(map[*gatewayworkspace.Runtime]*WorkspaceHandle),
		identity:       &componentIdentity{},
		observation:    observationapp.New(),
	}
	component.workspace = gatewayworkspace.NewComponent(dataPath, func(runtime *gatewayworkspace.Runtime) gatewayworkspace.Identity {
		identity := describe(component.handleFor(runtime))
		return gatewayworkspace.Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
	})
	return component
}

func (component *Component) handleFor(owner *gatewayworkspace.Runtime) *WorkspaceHandle {
	if component == nil || owner == nil {
		return nil
	}
	component.runtimeMu.Lock()
	defer component.runtimeMu.Unlock()
	if handle := component.handlesByOwner[owner]; handle != nil {
		return handle
	}
	handle := newWorkspaceHandle(component.identity, owner)
	component.handlesByOwner[owner] = handle
	return handle
}

func (component *Component) owns(handle *WorkspaceHandle) bool {
	return component != nil && handle != nil && handle.component == component.identity && handle.active.Load()
}

func (component *Component) forgetHandle(handle *WorkspaceHandle) {
	if !component.owns(handle) {
		return
	}
	component.runtimeMu.Lock()
	if component.handlesByOwner[handle.workspace] == handle {
		delete(component.handlesByOwner, handle.workspace)
		handle.active.Store(false)
	}
	component.runtimeMu.Unlock()
}

func (component *WorkspaceOwner) ConfigureWorkspaceLifecycle(dependencies WorkspaceDependencies) error {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return InitializationError()
	}
	var open func(string, string, string) (*gatewayworkspace.Runtime, error)
	if dependencies.Open != nil {
		open = func(path, id, password string) (*gatewayworkspace.Runtime, error) {
			runtime, err := dependencies.Open(path, id, password)
			if err != nil {
				return nil, err
			}
			owner, ok := component.resolve(runtime)
			if !ok {
				return nil, InitializationError()
			}
			return owner, nil
		}
	}
	var closeRuntime func(*gatewayworkspace.Runtime) error
	if dependencies.Close != nil {
		closeRuntime = func(runtime *gatewayworkspace.Runtime) error {
			return dependencies.Close(component.handleFor(runtime))
		}
	}
	var onActivated, onOpened func(*gatewayworkspace.Runtime)
	if dependencies.OnActivated != nil {
		onActivated = func(runtime *gatewayworkspace.Runtime) { dependencies.OnActivated(component.handleFor(runtime)) }
	}
	if dependencies.OnOpened != nil {
		onOpened = func(runtime *gatewayworkspace.Runtime) { dependencies.OnOpened(component.handleFor(runtime)) }
	}
	return component.owner.workspace.Configure(gatewayworkspace.Dependencies{
		DataPath: dependencies.DataPath,
		Open:     open, Close: closeRuntime, OnActivated: onActivated, OnOpened: onOpened,
		Move: dependencies.Move, Delete: dependencies.Delete,
		ValidateNewPassword: dependencies.ValidateNewPassword,
		Publish:             dependencies.Publish, GatewaySecret: dependencies.GatewaySecret,
	})
}

func (component *WorkspaceOwner) WorkspaceLifecycle() WorkspaceLifecyclePort {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil
	}
	return component.owner.workspace
}

func (component *WorkspaceOwner) WorkspaceIsUnlocked() bool {
	return component != nil && component.owner != nil && component.owner.workspace != nil && component.owner.workspace.IsUnlocked()
}

func (component *WorkspaceOwner) WorkspaceSelection() Identity {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return Identity{}
	}
	identity := component.owner.workspace.Selection()
	return Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
}

func (component *WorkspaceOwner) LookupWorkspace(id string) (*WorkspaceHandle, bool) {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil, false
	}
	owner, ok := component.owner.workspace.Lookup(id)
	return component.handleFor(owner), ok
}

func (component *WorkspaceOwner) ActivateWorkspace(handle *WorkspaceHandle) {
	if owner, ok := component.resolve(handle); ok && component.owner.workspace != nil {
		component.owner.workspace.Activate(owner)
	}
}

func (component *WorkspaceOwner) ActiveWorkspace() *WorkspaceHandle {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil
	}
	return component.handleFor(component.owner.workspace.Active())
}

func (component *WorkspaceOwner) WorkspaceSnapshot() []*WorkspaceHandle {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil
	}
	items := component.owner.workspace.Snapshot()
	runtimes := make([]*WorkspaceHandle, len(items))
	for index, runtime := range items {
		runtimes[index] = component.handleFor(runtime)
	}
	return runtimes
}

func (component *WorkspaceOwner) WorkspaceCount() int {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return 0
	}
	return component.owner.workspace.Len()
}

func (component *WorkspaceOwner) AdoptWorkspace(ctx context.Context, input gatewayworkspace.AdoptInput) (*WorkspaceHandle, error) {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.owner.workspace.Adopt(ctx, input)
	return component.handleFor(owner), err
}

func (component *WorkspaceOwner) OpenWorkspace(ctx context.Context, input OpenWorkspaceInput) (*WorkspaceHandle, error) {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.owner.workspace.Open(ctx, input.input)
	return component.handleFor(owner), err
}

func (component *WorkspaceOwner) DiscardWorkspace(handle *WorkspaceHandle, resolveTransfers func() TransferWorkflow, onComplete func()) error {
	owner, ok := component.resolve(handle)
	if !ok || component.owner.workspace == nil {
		return InitializationError()
	}
	var transfers func() gatewayworkspace.TransferWorkflow
	if resolveTransfers != nil {
		transfers = func() gatewayworkspace.TransferWorkflow { return resolveTransfers() }
	}
	err := component.owner.workspace.Discard(owner, transfers, func() {
		component.forgetHandle(handle)
		if onComplete != nil {
			onComplete()
		}
	})
	return err
}

func (component *WorkspaceOwner) CloseWorkspace(handle *WorkspaceHandle, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error), resolveTransfers func() TransferWorkflow, onComplete func()) error {
	owner, ok := component.resolve(handle)
	if !ok || component.owner.workspace == nil {
		return InitializationError()
	}
	var actions func() (gatewayworkspace.ActionWorkflow, error)
	if resolveActions != nil {
		actions = func() (gatewayworkspace.ActionWorkflow, error) { return resolveActions() }
	}
	var commands func() (gatewayworkspace.CommandWorkflow, error)
	if resolveCommands != nil {
		commands = func() (gatewayworkspace.CommandWorkflow, error) { return resolveCommands() }
	}
	var transfers func() gatewayworkspace.TransferWorkflow
	if resolveTransfers != nil {
		transfers = func() gatewayworkspace.TransferWorkflow { return resolveTransfers() }
	}
	err := component.owner.workspace.Close(owner, actions, commands, transfers, func() {
		component.forgetHandle(handle)
		if onComplete != nil {
			onComplete()
		}
	})
	return err
}

func (component *WorkspaceOwner) WaitWorkspaceClosed(ctx context.Context, handle *WorkspaceHandle) error {
	if component == nil || !component.belongs(handle) || handle.workspace == nil {
		return InitializationError()
	}
	return handle.workspace.WaitTeardown(ctx)
}

func (component *WorkspaceOwner) ConfiguredGatewaySecret(handle *WorkspaceHandle) string {
	owner, ok := component.resolve(handle)
	if !ok {
		return ""
	}
	return owner.ConfiguredGatewaySecret()
}

func (component *WorkspaceOwner) MoveDatabase(currentPath, targetPath string) error {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return InitializationError()
	}
	return component.owner.workspace.Move(currentPath, targetPath)
}

func (component *WorkspaceOwner) PublishDatabase(sourcePath, targetPath string) error {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return InitializationError()
	}
	return component.owner.workspace.Publish(sourcePath, targetPath)
}

func (component *WorkspaceOwner) DeleteDatabase(path string) error {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return InitializationError()
	}
	return component.owner.workspace.Delete(path)
}

func (component *WorkspaceOwner) LooksPlaintext(path string) bool {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return false
	}
	return component.owner.workspace.LooksPlaintext(path)
}

func (component *WorkspaceOwner) WorkspaceDatabaseName() (string, error) {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return "", InitializationError()
	}
	return component.owner.workspace.DatabaseName()
}

func (component *WorkspaceOwner) WorkspaceHTTP(dependencies WorkspaceHTTPDependencies) WorkspaceHTTPHandlers {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return nil
	}
	converted := gatewayworkspace.HTTPDependencies{
		HasSession: dependencies.HasSession, IssueSession: dependencies.IssueSession,
		ClearSessions: dependencies.ClearSessions, CloseMaintenance: dependencies.CloseMaintenance,
		Now: dependencies.Now,
	}
	if dependencies.BeginAttempt != nil {
		converted.BeginAttempt = func(w http.ResponseWriter, r *http.Request) (gatewayworkspace.PasswordAttempt, bool) {
			return dependencies.BeginAttempt(w, r)
		}
	}
	return component.owner.workspace.HTTP(converted)
}

func (component *WorkspaceOwner) HasActiveRemoteBackup(ctx context.Context, database *sql.DB) (bool, error) {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return false, InitializationError()
	}
	return component.owner.workspace.HasActiveRemoteBackup(ctx, database)
}

func (component *WorkspaceOwner) ValidateRemoteBackupPassword(password, databaseName string) error {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return InitializationError()
	}
	return component.owner.workspace.ValidateRemoteBackupPassword(password, databaseName)
}

func (component *WorkspaceOwner) PasswordPolicyError(err error) error {
	if component == nil || component.owner == nil || component.owner.workspace == nil {
		return InitializationError()
	}
	return component.owner.workspace.PasswordPolicyError(err)
}

func (component *WorkspaceOwner) AcquireBackupOperation(ctx context.Context) (func(), error) {
	if component == nil || component.owner == nil {
		return nil, InitializationError()
	}
	return component.owner.backupOperations.acquire(ctx)
}
