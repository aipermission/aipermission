package gatewayinfrastructure

import (
	"context"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/gatewayoptions"
	"github.com/aipermission/aipermission/backend/internal/gatewaystate"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

// Component owns process-scoped gateway infrastructure. Callers compose
// behavior through its methods instead of sharing mutable state containers.
type Component struct {
	connectors                 gatewaystate.ConnectorState
	controls                   gatewaystate.ControlState
	workspaces                 gatewaystate.WorkspaceState
	runtimeInstanceIDGenerator func() (string, error)
}

func NewComponent(dataPath, frontendPort string, describe func(Runtime) Identity, options ...ServerOption) *Component {
	resolved := gatewayoptions.Resolve(options)
	return &Component{
		connectors: gatewaystate.NewConnectorState(resolved.Registry, resolved.AdapterRegistry),
		controls:   gatewaystate.NewControlState(frontendPort, resolved.MaintenanceConsole),
		workspaces: gatewaystate.WorkspaceState{
			Registry: gatewayworkspace.NewRegistry(dataPath, gatewayworkspace.DefaultID(dataPath), describe),
		},
		runtimeInstanceIDGenerator: resolved.RuntimeInstanceIDGenerator,
	}
}

func (component *Component) RuntimeInstanceIDGenerator() func() (string, error) {
	if component == nil {
		return nil
	}
	return component.runtimeInstanceIDGenerator
}

func (component *Component) ConnectorRegistry() *gatewayoptions.ConnectorRegistry {
	if component == nil {
		return nil
	}
	return component.connectors.Registry
}

func (component *Component) ConnectorAdapterRegistry() *gatewayoptions.ConnectorAdapterRegistry {
	if component == nil {
		return nil
	}
	return component.connectors.AdapterRegistry
}

func (component *Component) ConfigureWorkspaceLifecycle(dependencies gatewayworkspace.Dependencies) error {
	if component == nil || component.workspaces.Registry == nil {
		return gatewayworkspace.ErrInitialization
	}
	dependencies.Registry = component.workspaces.Registry
	lifecycle, err := gatewayworkspace.NewService(dependencies)
	if err != nil {
		return err
	}
	component.workspaces.Lifecycle = lifecycle
	return nil
}

func (component *Component) WorkspaceLifecycle() *gatewayworkspace.Service {
	if component == nil {
		return nil
	}
	return component.workspaces.Lifecycle
}

func (component *Component) WorkspaceIsUnlocked() bool {
	return component != nil && component.workspaces.Registry != nil && component.workspaces.Registry.IsUnlocked()
}

func (component *Component) WorkspaceSelection() gatewayworkspace.Identity {
	if component == nil || component.workspaces.Registry == nil {
		return gatewayworkspace.Identity{}
	}
	return component.workspaces.Registry.Selection()
}

func (component *Component) LookupWorkspace(id string) (Runtime, bool) {
	if component == nil || component.workspaces.Registry == nil {
		return nil, false
	}
	return component.workspaces.Registry.Lookup(id)
}

func (component *Component) ActivateWorkspace(runtime Runtime) {
	if component != nil && component.workspaces.Registry != nil {
		component.workspaces.Registry.Activate(runtime)
	}
}

func (component *Component) ActiveWorkspace() Runtime {
	if component == nil || component.workspaces.Registry == nil {
		return nil
	}
	if component.workspaces.Lifecycle != nil {
		runtime, _ := component.workspaces.Lifecycle.Active()
		return runtime
	}
	runtime, _ := component.workspaces.Registry.Active()
	return runtime
}

func (component *Component) WorkspaceSnapshot() []Runtime {
	if component == nil || component.workspaces.Registry == nil {
		return nil
	}
	if component.workspaces.Lifecycle != nil {
		return component.workspaces.Lifecycle.Snapshot()
	}
	return component.workspaces.Registry.Snapshot()
}

func (component *Component) WorkspaceCount() int {
	if component == nil || component.workspaces.Registry == nil {
		return 0
	}
	return component.workspaces.Registry.Len()
}

func (component *Component) DatabasePasswordLimiter() *runtimecontrol.Auth {
	return component.controls.AuthLimiter
}

func (component *Component) MCPIPLimiter() *runtimecontrol.Auth {
	return component.controls.MCPIPAuthLimiter
}

func (component *Component) MCPTokenLimiter() *runtimecontrol.Auth {
	return component.controls.MCPTokenAuthLimiter
}

func (component *Component) AllowVaultReveal(key string) bool {
	return component != nil && component.controls.VaultRevealLimiter != nil && component.controls.VaultRevealLimiter.Allow(key)
}

func (component *Component) AllowVaultGenerate(key string) bool {
	return component != nil && component.controls.VaultGenerateLimiter != nil && component.controls.VaultGenerateLimiter.Allow(key)
}

func (component *Component) AllowVaultRequest(key string) bool {
	return component != nil && component.controls.VaultRequestLimiter != nil && component.controls.VaultRequestLimiter.Allow(key)
}

func (component *Component) ConfigureVaultRequestLimiter(limiter *runtimecontrol.Window) {
	if component != nil {
		component.controls.VaultRequestLimiter = limiter
	}
}

func (component *Component) IssueUISession(w http.ResponseWriter, databaseID, retryIdentity string) error {
	return component.controls.UISessions.Issue(w, databaseID, retryIdentity)
}

func (component *Component) IssuePreparedUISession(w http.ResponseWriter, prepared uisession.Prepared, databaseID, retryIdentity string) error {
	return component.controls.UISessions.IssuePrepared(w, prepared, databaseID, retryIdentity)
}

func (component *Component) ClearUISessions(w http.ResponseWriter) {
	component.controls.UISessions.Clear(w)
}

func (component *Component) ValidUISession(r *http.Request, databaseID string) bool {
	return component.controls.UISessions.Valid(r, databaseID)
}

func (component *Component) ValidUICSRF(r *http.Request) bool {
	return component.controls.UISessions.ValidCSRF(r)
}

func (component *Component) EnsureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request, retryIdentity string) {
	component.controls.UISessions.EnsureWorkspaceCookie(w, r, retryIdentity)
}

func (component *Component) MaintenanceConsole() gatewayoptions.MaintenanceConsoleRuntime {
	if component == nil {
		return nil
	}
	return component.controls.MaintenanceConsole
}

func (component *Component) AcquireBackupOperation(ctx context.Context) (func(), error) {
	return component.controls.BackupOperations.Acquire(ctx)
}
