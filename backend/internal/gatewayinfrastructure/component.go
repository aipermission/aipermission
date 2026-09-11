package gatewayinfrastructure

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/uisession"
)

// Component owns process-scoped gateway infrastructure. Callers compose
// behavior through its methods instead of sharing mutable state containers.
type Component struct {
	connectorRegistry          *connectors.Registry
	connectorAdapterRegistry   *connectorapi.Registry
	maintenanceConsole         console.MaintenanceConsoleRuntime
	databasePasswordLimiter    *runtimecontrol.Auth
	mcpIPLimiter               *runtimecontrol.Auth
	mcpTokenLimiter            *runtimecontrol.Auth
	vaultRevealLimiter         *runtimecontrol.Window
	vaultGenerateLimiter       *runtimecontrol.Window
	vaultRequestLimiter        *runtimecontrol.Window
	uiSessions                 *uisession.Manager
	backupOperations           backups.OperationLimiter
	workspace                  *gatewayworkspace.Component
	runtimeInstanceIDGenerator func() (string, error)
}

func NewComponent(dataPath, frontendPort string, describe func(Runtime) Identity, options ...ServerOption) *Component {
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
		connectorRegistry:          resolved.Registry,
		connectorAdapterRegistry:   resolved.AdapterRegistry,
		maintenanceConsole:         resolved.MaintenanceConsole,
		databasePasswordLimiter:    runtimecontrol.NewAuth(1, AuthLockoutFailures),
		mcpIPLimiter:               runtimecontrol.NewAuth(MCPGlobalDelayFailures, MCPGlobalLockoutFailures),
		mcpTokenLimiter:            runtimecontrol.NewAuth(1, AuthLockoutFailures),
		vaultRevealLimiter:         runtimecontrol.NewWindow(8, time.Minute),
		vaultGenerateLimiter:       runtimecontrol.NewWindow(10, time.Minute),
		vaultRequestLimiter:        runtimecontrol.NewWindow(30, time.Minute),
		uiSessions:                 uisession.New(frontendPort),
		workspace:                  gatewayworkspace.NewComponent(dataPath, describe),
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

func (component *Component) ConfigureWorkspaceLifecycle(dependencies gatewayworkspace.Dependencies) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.Configure(dependencies)
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
	return component.workspace.Snapshot()
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

func (component *Component) CloseWorkspace(runtime Runtime, resolve func() (ActionWorkflow, error)) error {
	if component == nil || component.workspace == nil {
		return gatewayworkspace.ErrInitialization
	}
	return component.workspace.Close(runtime, resolve)
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

func (component *Component) WaitDatabasePassword(ctx context.Context, key string) error {
	return component.databasePasswordLimiter.Wait(ctx, key)
}

func (component *Component) RecordDatabasePasswordFailure(key string) {
	component.databasePasswordLimiter.RecordFailure(key)
}

func (component *Component) RecordDatabasePasswordSuccess(key string) {
	component.databasePasswordLimiter.RecordSuccess(key)
}

func (component *Component) DatabasePasswordFailureCount(key string) int {
	return component.databasePasswordLimiter.FailureCount(key)
}

func (component *Component) WaitMCPIP(ctx context.Context, key string) error {
	return component.mcpIPLimiter.Wait(ctx, key)
}

func (component *Component) RecordMCPIPFailure(key string) {
	component.mcpIPLimiter.RecordFailure(key)
}

func (component *Component) RecordMCPIPSuccess(key string) {
	component.mcpIPLimiter.RecordSuccess(key)
}

func (component *Component) WaitMCPToken(ctx context.Context, key string) error {
	return component.mcpTokenLimiter.Wait(ctx, key)
}

func (component *Component) RecordMCPTokenFailure(key string) {
	component.mcpTokenLimiter.RecordFailure(key)
}

func (component *Component) RecordMCPTokenSuccess(key string) {
	component.mcpTokenLimiter.RecordSuccess(key)
}

func (component *Component) AllowVaultReveal(key string) bool {
	return component != nil && component.vaultRevealLimiter != nil && component.vaultRevealLimiter.Allow(key)
}

func (component *Component) AllowVaultGenerate(key string) bool {
	return component != nil && component.vaultGenerateLimiter != nil && component.vaultGenerateLimiter.Allow(key)
}

func (component *Component) AllowVaultRequest(key string) bool {
	return component != nil && component.vaultRequestLimiter != nil && component.vaultRequestLimiter.Allow(key)
}

func (component *Component) ConfigureVaultRequestLimit(limit int, window time.Duration) {
	if component != nil {
		component.vaultRequestLimiter = runtimecontrol.NewWindow(limit, window)
	}
}

func (component *Component) IssueUISession(w http.ResponseWriter, databaseID, retryIdentity string) error {
	return component.uiSessions.Issue(w, databaseID, retryIdentity)
}

func (component *Component) IssuePreparedUISession(w http.ResponseWriter, prepared uisession.Prepared, databaseID, retryIdentity string) error {
	return component.uiSessions.IssuePrepared(w, prepared, databaseID, retryIdentity)
}

func (component *Component) ClearUISessions(w http.ResponseWriter) {
	component.uiSessions.Clear(w)
}

func (component *Component) ValidUISession(r *http.Request, databaseID string) bool {
	return component.uiSessions.Valid(r, databaseID)
}

func (component *Component) ValidUICSRF(r *http.Request) bool {
	return component.uiSessions.ValidCSRF(r)
}

func (component *Component) EnsureUIWorkspaceCookie(w http.ResponseWriter, r *http.Request, retryIdentity string) {
	component.uiSessions.EnsureWorkspaceCookie(w, r, retryIdentity)
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
