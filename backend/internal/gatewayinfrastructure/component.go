package gatewayinfrastructure

import (
	"context"
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
	accessOwner      *AccessOwner
	actionOwner      *ConnectorActionOwner
	managementOwner  *ConnectorManagementOwner
	portsOwner       *ConnectorPortsOwner
	observationOwner *ObservationOwner
	operationsOwner  *OperationsOwner
	vaultOwner       *VaultOwner
	workspaceOwner   *WorkspaceOwner
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
	component.bindOwners()
	component.workspace = gatewayworkspace.NewComponent(dataPath, func(runtime *gatewayworkspace.Runtime) gatewayworkspace.Identity {
		identity := describe(component.handleFor(runtime))
		return gatewayworkspace.Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
	})
	component.bindWorkspaceOwner()
	component.operationsOwner.backupLifecycle = component.workspace
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
	component.bindHandleCapabilities(handle, gatewayworkspace.ProjectCapabilities(owner))
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
		component.forgetHandleCapabilities(handle)
		handle.active.Store(false)
	}
	component.runtimeMu.Unlock()
}

func (component *Component) ownedHandlesSnapshot() []*WorkspaceHandle {
	if component == nil {
		return nil
	}
	component.runtimeMu.Lock()
	defer component.runtimeMu.Unlock()
	result := make([]*WorkspaceHandle, 0, len(component.handlesByOwner))
	for _, handle := range component.handlesByOwner {
		if handle != nil && handle.active.Load() {
			result = append(result, handle)
		}
	}
	return result
}

func (component *WorkspaceOwner) ownsWorkspaceIdentity(identity gatewayworkspace.Identity) bool {
	if component == nil || component.ownedHandles == nil {
		return false
	}
	for _, handle := range component.ownedHandles() {
		if handle == nil || !handle.active.Load() {
			continue
		}
		candidate := handle.WorkspaceIdentity()
		if candidate.ID == identity.ID && candidate.Path == identity.Path {
			return true
		}
	}
	return false
}

func (component *WorkspaceOwner) ConfigureWorkspaceLifecycle(dependencies WorkspaceDependencies) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	var open func(context.Context, string, string, string) (*gatewayworkspace.Runtime, error)
	if dependencies.Open != nil {
		open = func(ctx context.Context, path, id, password string) (*gatewayworkspace.Runtime, error) {
			runtime, err := dependencies.Open(ctx, path, id, password)
			if err != nil {
				return nil, err
			}
			owner, ok := component.lifecycleRuntime(runtime)
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
	return component.workspace.Configure(gatewayworkspace.Dependencies{
		DataPath: dependencies.DataPath,
		Open:     open, Close: closeRuntime, OnActivated: onActivated, OnOpened: onOpened,
		WaitClosed: func(ctx context.Context, runtime *gatewayworkspace.Runtime) error {
			return component.WaitWorkspaceClosed(ctx, component.handleFor(runtime))
		},
		IsOwned: component.ownsWorkspaceIdentity,
		Move:    dependencies.Move, Delete: dependencies.Delete,
		ValidateNewPassword: func(ctx context.Context, runtime *gatewayworkspace.Runtime, databaseName, password string) error {
			return component.validatePassword(ctx, runtime, databaseName, password)
		},
		Publish: dependencies.Publish, GatewaySecret: dependencies.GatewaySecret,
	})
}

func (component *WorkspaceOwner) WorkspaceLifecycle() WorkspaceLifecyclePort {
	if component == nil || component.workspace == nil {
		return nil
	}
	return component.workspace
}

func (component *WorkspaceOwner) WorkspaceIsUnlocked() bool {
	return component != nil && component.workspace != nil && component.workspace.IsUnlocked()
}

func (component *WorkspaceOwner) WorkspaceSelection() Identity {
	if component == nil || component.workspace == nil {
		return Identity{}
	}
	identity := component.workspace.Selection()
	return Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
}

func (component *WorkspaceOwner) LookupWorkspace(id string) (*WorkspaceHandle, bool) {
	if component == nil || component.workspace == nil {
		return nil, false
	}
	owner, ok := component.workspace.Lookup(id)
	return component.handleFor(owner), ok
}

func (component *WorkspaceOwner) ActivateWorkspace(handle *WorkspaceHandle) {
	if owner, ok := component.lifecycleRuntime(handle); ok && component.workspace != nil {
		component.workspace.Activate(owner)
	}
}

func (component *WorkspaceOwner) ActiveWorkspace() *WorkspaceHandle {
	if component == nil || component.workspace == nil {
		return nil
	}
	return component.handleFor(component.workspace.Active())
}

func (component *WorkspaceOwner) WorkspaceSnapshot() []*WorkspaceHandle {
	if component == nil || component.workspace == nil {
		return nil
	}
	items := component.workspace.Snapshot()
	runtimes := make([]*WorkspaceHandle, len(items))
	for index, runtime := range items {
		runtimes[index] = component.handleFor(runtime)
	}
	return runtimes
}

// OwnedWorkspaceSnapshot includes runtimes removed from the public lifecycle
// registry while their deferred teardown is still owned by this process.
func (component *WorkspaceOwner) OwnedWorkspaceSnapshot() []*WorkspaceHandle {
	if component == nil || component.ownedHandles == nil {
		return nil
	}
	return component.ownedHandles()
}

func (component *WorkspaceOwner) WorkspaceCount() int {
	if component == nil || component.workspace == nil {
		return 0
	}
	return component.workspace.Len()
}

func (component *WorkspaceOwner) AdoptWorkspace(ctx context.Context, input gatewayworkspace.AdoptInput) (*WorkspaceHandle, error) {
	if component == nil || component.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.workspace.Adopt(ctx, input)
	return component.handleFor(owner), err
}

func (component *WorkspaceOwner) OpenWorkspace(ctx context.Context, input OpenWorkspaceInput) (*WorkspaceHandle, error) {
	if component == nil || component.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.workspace.Open(ctx, input.input)
	return component.handleFor(owner), err
}

func (component *WorkspaceOwner) DiscardWorkspace(handle *WorkspaceHandle, resolveTransfers func() TransferWorkflow, onComplete func()) error {
	owner, ok := component.lifecycleRuntime(handle)
	if !ok || component.workspace == nil {
		return InitializationError()
	}
	var transfers func() gatewayworkspace.TransferWorkflow
	if resolveTransfers != nil {
		transfers = func() gatewayworkspace.TransferWorkflow { return resolveTransfers() }
	}
	err := component.workspace.Discard(owner, transfers, func() {
		if onComplete != nil {
			onComplete()
		}
		component.forgetHandle(handle)
	})
	return err
}

func (component *WorkspaceOwner) CloseWorkspace(handle *WorkspaceHandle, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error), resolveTransfers func() TransferWorkflow, onComplete func()) error {
	owner, ok := component.lifecycleRuntime(handle)
	if !ok || component.workspace == nil {
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
	err := component.workspace.Close(owner, actions, commands, transfers, func() {
		if onComplete != nil {
			onComplete()
		}
		component.forgetHandle(handle)
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
	owner, ok := component.lifecycleRuntime(handle)
	if !ok {
		return ""
	}
	return owner.ConfiguredGatewaySecret()
}

func (component *WorkspaceOwner) MoveDatabase(currentPath, targetPath string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.Move(currentPath, targetPath)
}

func (component *WorkspaceOwner) PublishDatabase(sourcePath, targetPath string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.Publish(sourcePath, targetPath)
}

func (component *WorkspaceOwner) DeleteDatabase(path string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.Delete(path)
}

func (component *WorkspaceOwner) LooksPlaintext(path string) bool {
	if component == nil || component.workspace == nil {
		return false
	}
	return component.workspace.LooksPlaintext(path)
}

func (component *WorkspaceOwner) WorkspaceDatabaseName() (string, error) {
	if component == nil || component.workspace == nil {
		return "", InitializationError()
	}
	return component.workspace.DatabaseName()
}

func (component *WorkspaceOwner) WorkspaceHTTP(dependencies WorkspaceHTTPDependencies) WorkspaceHTTPHandlers {
	if component == nil || component.workspace == nil {
		return nil
	}
	converted := gatewayworkspace.HTTPDependencies{
		HasSession: dependencies.HasSession, IssueSession: dependencies.IssueSession,
		ClearSessions: dependencies.ClearSessions, InvalidateSessions: dependencies.InvalidateSessions,
		CloseMaintenance: dependencies.CloseMaintenance,
		Now:              dependencies.Now,
	}
	if dependencies.BeginAttempt != nil {
		converted.BeginAttempt = func(w http.ResponseWriter, r *http.Request) (gatewayworkspace.PasswordAttempt, bool) {
			return dependencies.BeginAttempt(w, r)
		}
	}
	return component.workspace.HTTP(converted)
}

func (component *Component) acquireBackupOperation(ctx context.Context) (func(), error) {
	if component == nil {
		return nil, InitializationError()
	}
	return component.backupOperations.acquire(ctx)
}
