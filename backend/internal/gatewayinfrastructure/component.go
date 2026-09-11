package gatewayinfrastructure

import (
	"context"
	"database/sql"

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
}

func NewComponent(dataPath string, describe func(Runtime) Identity, options ...ServerOption) *Component {
	if describe == nil {
		describe = func(runtime Runtime) Identity {
			if runtime == nil {
				return Identity{}
			}
			return runtime.WorkspaceIdentity()
		}
	}
	resolved := resolveOptions(options)
	return &Component{
		connectorRegistry:        resolved.Registry,
		connectorAdapterRegistry: resolved.AdapterRegistry,
		maintenanceConsole:       resolved.MaintenanceConsole,
		workspace: gatewayworkspace.NewComponent(dataPath, func(runtime gatewayworkspace.Runtime) gatewayworkspace.Identity {
			return describe(runtime)
		}),
		runtimeInstanceIDGenerator: resolved.RuntimeInstanceIDGenerator,
	}
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
		return gatewayworkspace.ErrInitialization
	}
	var open func(string, string, string) (gatewayworkspace.Runtime, error)
	if dependencies.Open != nil {
		open = func(path, id, password string) (gatewayworkspace.Runtime, error) {
			return dependencies.Open(path, id, password)
		}
	}
	var closeRuntime func(gatewayworkspace.Runtime) error
	if dependencies.Close != nil {
		closeRuntime = func(runtime gatewayworkspace.Runtime) error { return dependencies.Close(runtime) }
	}
	var onActivated, onOpened func(gatewayworkspace.Runtime)
	if dependencies.OnActivated != nil {
		onActivated = func(runtime gatewayworkspace.Runtime) { dependencies.OnActivated(runtime) }
	}
	if dependencies.OnOpened != nil {
		onOpened = func(runtime gatewayworkspace.Runtime) { dependencies.OnOpened(runtime) }
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

func (component *Component) WorkspaceSelection() gatewayworkspace.Identity {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.Identity{}
	}
	return component.workspace.Selection()
}

func (component *Component) LookupWorkspace(id string) (Runtime, bool) {
	if component == nil || component.workspace == nil {
		return nil, false
	}
	return component.workspace.Lookup(id)
}

func (component *Component) ActivateWorkspace(runtime Runtime) {
	if component != nil && component.workspace != nil && runtime != nil {
		component.workspace.Activate(runtime)
	}
}

func (component *Component) ActiveWorkspace() Runtime {
	if component == nil || component.workspace == nil {
		return nil
	}
	return component.workspace.Active()
}

func (component *Component) WorkspaceSnapshot() []Runtime {
	if component == nil || component.workspace == nil {
		return nil
	}
	items := component.workspace.Snapshot()
	runtimes := make([]Runtime, len(items))
	for index, runtime := range items {
		runtimes[index] = runtime
	}
	return runtimes
}

func (component *Component) WorkspaceCount() int {
	if component == nil || component.workspace == nil {
		return 0
	}
	return component.workspace.Len()
}

func (component *Component) AdoptWorkspace(ctx context.Context, input AdoptInput) (Runtime, error) {
	if component == nil || component.workspace == nil {
		return nil, gatewayworkspace.ErrInitialization
	}
	return component.workspace.Adopt(ctx, input)
}

func (component *Component) OpenWorkspace(ctx context.Context, input OpenInput) (Runtime, error) {
	if component == nil || component.workspace == nil {
		return nil, gatewayworkspace.ErrInitialization
	}
	return component.workspace.Open(ctx, input)
}

func (component *Component) DiscardWorkspace(runtime Runtime) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.Discard(runtime)
}

func (component *Component) CloseWorkspace(runtime Runtime, resolveActions func() (ActionWorkflow, error), resolveCommands func() (CommandWorkflow, error)) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.Close(runtime, resolveActions, resolveCommands)
}

func (component *Component) MoveDatabase(currentPath, targetPath string) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.Move(currentPath, targetPath)
}

func (component *Component) PublishDatabase(sourcePath, targetPath string) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.Publish(sourcePath, targetPath)
}

func (component *Component) DeleteDatabase(path string) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
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
		return "", gatewayworkspace.ErrInitialization
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
		return false, gatewayworkspace.ErrInitialization
	}
	return component.workspace.HasActiveRemoteBackup(ctx, database)
}

func (component *Component) ValidateRemoteBackupPassword(password, databaseName string) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.ValidateRemoteBackupPassword(password, databaseName)
}

func (component *Component) PasswordPolicyError(err error) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
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
		return nil, gatewayworkspace.ErrInitialization
	}
	return component.backupOperations.Acquire(ctx)
}
