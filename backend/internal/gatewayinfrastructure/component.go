package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"sync"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
)

// Component owns process-scoped gateway infrastructure. Callers compose
// behavior through its methods instead of sharing mutable state containers.
type Component struct {
	connectorRegistry          *connectors.Registry
	connectorAdapterRegistry   *connectorapi.Registry
	maintenanceConsole         console.MaintenanceConsoleRuntime
	backupOperations           backups.OperationLimiter
	workspace                  *gatewayworkspace.Component
	runtimeInstanceIDGenerator func() (string, error)
	runtimeMu                  sync.Mutex
	runtimes                   map[*gatewayworkspace.Runtime]*Runtime
}

func NewComponent(dataPath string, describe func(*Runtime) Identity, options ...ServerOption) *Component {
	if describe == nil {
		describe = func(runtime *Runtime) Identity {
			if runtime == nil {
				return Identity{}
			}
			return runtime.WorkspaceIdentity()
		}
	}
	resolved := resolveOptions(options)
	component := &Component{
		connectorRegistry:          resolved.Registry,
		connectorAdapterRegistry:   resolved.AdapterRegistry,
		maintenanceConsole:         resolved.MaintenanceConsole,
		runtimes:                   make(map[*gatewayworkspace.Runtime]*Runtime),
		runtimeInstanceIDGenerator: resolved.RuntimeInstanceIDGenerator,
	}
	component.workspace = gatewayworkspace.NewComponent(dataPath, func(runtime *gatewayworkspace.Runtime) Identity {
		return describe(component.wrapRuntime(runtime))
	})
	return component
}

func (component *Component) wrapRuntime(owner *gatewayworkspace.Runtime) *Runtime {
	if component == nil || owner == nil {
		return nil
	}
	component.runtimeMu.Lock()
	defer component.runtimeMu.Unlock()
	if runtime := component.runtimes[owner]; runtime != nil {
		return runtime
	}
	runtime := wrapRuntime(owner)
	component.runtimes[owner] = runtime
	return runtime
}

func (component *Component) forgetRuntime(runtime *Runtime) {
	if component == nil || runtime == nil || runtime.owner == nil {
		return
	}
	component.runtimeMu.Lock()
	delete(component.runtimes, runtime.owner)
	component.runtimeMu.Unlock()
}

func (component *Component) RuntimeInstanceIDGenerator() func() (string, error) {
	if component == nil {
		return nil
	}
	return component.runtimeInstanceIDGenerator
}

func (component *Component) ConnectorRegistry() *connectors.Registry {
	if component == nil {
		return nil
	}
	return component.connectorRegistry
}

func (component *Component) ConnectorAdapterRegistry() *connectorapi.Registry {
	if component == nil {
		return nil
	}
	return component.connectorAdapterRegistry
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
			if runtime == nil || runtime.owner == nil {
				return nil, InitializationError()
			}
			return runtime.owner, nil
		}
	}
	var closeRuntime func(*gatewayworkspace.Runtime) error
	if dependencies.Close != nil {
		closeRuntime = func(runtime *gatewayworkspace.Runtime) error {
			return dependencies.Close(component.wrapRuntime(runtime))
		}
	}
	var onActivated, onOpened func(*gatewayworkspace.Runtime)
	if dependencies.OnActivated != nil {
		onActivated = func(runtime *gatewayworkspace.Runtime) { dependencies.OnActivated(component.wrapRuntime(runtime)) }
	}
	if dependencies.OnOpened != nil {
		onOpened = func(runtime *gatewayworkspace.Runtime) { dependencies.OnOpened(component.wrapRuntime(runtime)) }
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
	return component.workspace.Selection()
}

func (component *Component) LookupWorkspace(id string) (*Runtime, bool) {
	if component == nil || component.workspace == nil {
		return nil, false
	}
	owner, ok := component.workspace.Lookup(id)
	return component.wrapRuntime(owner), ok
}

func (component *Component) ActivateWorkspace(runtime *Runtime) {
	if component != nil && component.workspace != nil && runtime != nil && runtime.owner != nil {
		component.workspace.Activate(runtime.owner)
	}
}

func (component *Component) ActiveWorkspace() *Runtime {
	if component == nil || component.workspace == nil {
		return nil
	}
	return component.wrapRuntime(component.workspace.Active())
}

func (component *Component) WorkspaceSnapshot() []*Runtime {
	if component == nil || component.workspace == nil {
		return nil
	}
	items := component.workspace.Snapshot()
	runtimes := make([]*Runtime, len(items))
	for index, runtime := range items {
		runtimes[index] = component.wrapRuntime(runtime)
	}
	return runtimes
}

func (component *Component) WorkspaceCount() int {
	if component == nil || component.workspace == nil {
		return 0
	}
	return component.workspace.Len()
}

func (component *Component) AdoptWorkspace(ctx context.Context, input AdoptInput) (*Runtime, error) {
	if component == nil || component.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.workspace.Adopt(ctx, input)
	return component.wrapRuntime(owner), err
}

func (component *Component) OpenWorkspace(ctx context.Context, input OpenInput) (*Runtime, error) {
	if component == nil || component.workspace == nil {
		return nil, InitializationError()
	}
	owner, err := component.workspace.Open(ctx, input)
	return component.wrapRuntime(owner), err
}

func (component *Component) DiscardWorkspace(runtime *Runtime, resolveTransfers func() TransferWorkflow) error {
	if component == nil || component.workspace == nil || runtime == nil || runtime.owner == nil {
		return InitializationError()
	}
	err := component.workspace.Discard(runtime.owner, resolveTransfers)
	component.forgetRuntime(runtime)
	return err
}

func (component *Component) CloseWorkspace(runtime *Runtime, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error), resolveTransfers func() TransferWorkflow) error {
	if component == nil || component.workspace == nil || runtime == nil || runtime.owner == nil {
		return InitializationError()
	}
	err := component.workspace.Close(runtime.owner, resolveActions, resolveCommands, resolveTransfers)
	component.forgetRuntime(runtime)
	return err
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
	return component.workspace.HTTP(dependencies)
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

func (component *Component) MaintenanceConsole() console.MaintenanceConsoleRuntime {
	if component == nil {
		return nil
	}
	return component.maintenanceConsole
}

func (component *Component) AcquireBackupOperation(ctx context.Context) (func(), error) {
	if component == nil {
		return nil, InitializationError()
	}
	return component.backupOperations.Acquire(ctx)
}
