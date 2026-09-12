package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/bootstrap"
	observationapp "github.com/aipermission/aipermission/backend/internal/gatewayoperations/observation"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

// Component owns process-scoped gateway infrastructure. Callers compose
// behavior through its methods instead of sharing mutable state containers.
type Component struct {
	backupOperations backupOperationLimiter
	observation      *observationapp.Component
	workspace        *gatewayworkspace.Component
	runtimeMu        sync.Mutex
	handlesByOwner   map[*gatewayworkspace.Runtime]*WorkspaceHandle
	ownersByToken    map[*workspaceToken]*gatewayworkspace.Runtime
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
		ownersByToken:  make(map[*workspaceToken]*gatewayworkspace.Runtime),
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
	handle := newWorkspaceHandle(owner)
	component.handlesByOwner[owner] = handle
	component.ownersByToken[handle.token] = owner
	return handle
}

func (component *Component) resolve(handle *WorkspaceHandle) (*gatewayworkspace.Runtime, bool) {
	if component == nil || handle == nil || handle.token == nil {
		return nil, false
	}
	component.runtimeMu.Lock()
	defer component.runtimeMu.Unlock()
	owner, ok := component.ownersByToken[handle.token]
	return owner, ok && component.handlesByOwner[owner] == handle
}

func (component *Component) forgetHandle(handle *WorkspaceHandle) {
	if component == nil || handle == nil || handle.token == nil {
		return
	}
	component.runtimeMu.Lock()
	if owner, ok := component.ownersByToken[handle.token]; ok {
		delete(component.ownersByToken, handle.token)
		delete(component.handlesByOwner, owner)
	}
	component.runtimeMu.Unlock()
}

func (component *Component) ConfigureWorkspaceLifecycle(dependencies WorkspaceDependencies) error {
	if component == nil || component.workspace == nil {
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
	return component.workspace.Configure(gatewayworkspace.Dependencies{
		DataPath: dependencies.DataPath,
		Open:     open, Close: closeRuntime, OnActivated: onActivated, OnOpened: onOpened,
		Move: dependencies.Move, Delete: dependencies.Delete,
		ValidateNewPassword: dependencies.ValidateNewPassword,
		Publish:             dependencies.Publish, GatewaySecret: dependencies.GatewaySecret,
	})
}

func (component *Component) WorkspaceLifecycle() WorkspaceLifecyclePort {
	if component == nil || component.workspace == nil {
		return nil
	}
	return component.workspace
}

func (component *Component) WorkspaceIsUnlocked() bool {
	return component != nil && component.workspace != nil && component.workspace.IsUnlocked()
}

func (component *Component) WorkspaceSelection() Identity {
	if component == nil || component.workspace == nil {
		return Identity{}
	}
	identity := component.workspace.Selection()
	return Identity{ID: identity.ID, Path: identity.Path, RetryIdentity: identity.RetryIdentity}
}

func (component *Component) LookupWorkspace(id string) (*WorkspaceHandle, bool) {
	if component == nil || component.workspace == nil {
		return nil, false
	}
	owner, ok := component.workspace.Lookup(id)
	return component.handleFor(owner), ok
}

func (component *Component) ActivateWorkspace(handle *WorkspaceHandle) {
	if owner, ok := component.resolve(handle); ok && component.workspace != nil {
		component.workspace.Activate(owner)
	}
}

func (component *Component) ActiveWorkspace() *WorkspaceHandle {
	if component == nil || component.workspace == nil {
		return nil
	}
	return component.handleFor(component.workspace.Active())
}

func (component *Component) WorkspaceSnapshot() []*WorkspaceHandle {
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

func (component *Component) WorkspaceCount() int {
	if component == nil || component.workspace == nil {
		return 0
	}
	return component.workspace.Len()
}

func (component *Component) AdoptWorkspace(ctx context.Context, input bootstrap.Adopt) (*WorkspaceHandle, error) {
	if component == nil || component.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.workspace.Adopt(ctx, input.WorkspaceInput())
	return component.handleFor(owner), err
}

func (component *Component) OpenWorkspace(ctx context.Context, input bootstrap.Open) (*WorkspaceHandle, error) {
	if component == nil || component.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.workspace.Open(ctx, input.WorkspaceInput())
	return component.handleFor(owner), err
}

func (component *Component) DiscardWorkspace(handle *WorkspaceHandle, resolveTransfers func() TransferWorkflow) error {
	owner, ok := component.resolve(handle)
	if !ok || component.workspace == nil {
		return InitializationError()
	}
	var transfers func() gatewayworkspace.TransferWorkflow
	if resolveTransfers != nil {
		transfers = func() gatewayworkspace.TransferWorkflow { return resolveTransfers() }
	}
	err := component.workspace.Discard(owner, transfers)
	component.forgetHandle(handle)
	return err
}

func (component *Component) CloseWorkspace(handle *WorkspaceHandle, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error), resolveTransfers func() TransferWorkflow) error {
	owner, ok := component.resolve(handle)
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
	err := component.workspace.Close(owner, actions, commands, transfers)
	component.forgetHandle(handle)
	return err
}

func (component *Component) ConfiguredGatewaySecret(handle *WorkspaceHandle) string {
	owner, ok := component.resolve(handle)
	if !ok {
		return ""
	}
	return owner.ConfiguredGatewaySecret()
}

func (component *Component) MoveDatabase(currentPath, targetPath string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.Move(currentPath, targetPath)
}

func (component *Component) PublishDatabase(sourcePath, targetPath string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.Publish(sourcePath, targetPath)
}

func (component *Component) DeleteDatabase(path string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.Delete(path)
}

func (component *Component) LooksPlaintext(path string) bool {
	if component == nil || component.workspace == nil {
		return false
	}
	return component.workspace.LooksPlaintext(path)
}

func (component *Component) WorkspaceDatabaseName() (string, error) {
	if component == nil || component.workspace == nil {
		return "", InitializationError()
	}
	return component.workspace.DatabaseName()
}

func (component *Component) WorkspaceHTTP(dependencies WorkspaceHTTPDependencies) WorkspaceHTTPHandlers {
	if component == nil || component.workspace == nil {
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
	return component.workspace.HTTP(converted)
}

func (component *Component) HasActiveRemoteBackup(ctx context.Context, database *sql.DB) (bool, error) {
	if component == nil || component.workspace == nil {
		return false, InitializationError()
	}
	return component.workspace.HasActiveRemoteBackup(ctx, database)
}

func (component *Component) ValidateRemoteBackupPassword(password, databaseName string) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.ValidateRemoteBackupPassword(password, databaseName)
}

func (component *Component) PasswordPolicyError(err error) error {
	if component == nil || component.workspace == nil {
		return InitializationError()
	}
	return component.workspace.PasswordPolicyError(err)
}

func (component *Component) AcquireBackupOperation(ctx context.Context) (func(), error) {
	if component == nil {
		return nil, InitializationError()
	}
	return component.backupOperations.acquire(ctx)
}
