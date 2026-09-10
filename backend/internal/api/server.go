package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/commandrequests"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectorruntime"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	filetransferhttp "github.com/aipermission/aipermission/backend/internal/filetransfer/httpapi"
	"github.com/aipermission/aipermission/backend/internal/observability"
	"github.com/aipermission/aipermission/backend/internal/projectvault"
	"github.com/aipermission/aipermission/backend/internal/retention"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
)

type Server struct {
	config               serverConfig
	workspaces           *workspacelifecycle.Registry[*databaseRuntime]
	workspaceLifecycle   *workspacelifecycle.Service[*databaseRuntime]
	registry             *connectors.Registry
	adapterRegistry      *connectorapi.Registry
	mux                  *http.ServeMux
	lifecycleMu          sync.RWMutex
	maintenanceConsole   console.MaintenanceConsoleRuntime
	authLimiter          *runtimecontrol.Auth
	mcpIPAuthLimiter     *runtimecontrol.Auth
	mcpTokenAuthLimiter  *runtimecontrol.Auth
	vaultRevealLimiter   *runtimecontrol.Window
	vaultGenerateLimiter *runtimecontrol.Window
	vaultRequestLimiter  *runtimecontrol.Window
	uiSessions           *uisession.Manager
	auditHealth          observability.HealthTracker
	databaseMove         func(string, string) error
	databasePublish      func(string, string) error
	runtimeOpen          func(string, string, string) (*databaseRuntime, error)
	backupOperations     backups.OperationLimiter
}

type databaseRuntime struct {
	id                 string
	path               string
	gatewaySecret      string
	database           *sql.DB
	vault              *vault.Vault
	tokens             *tokens.Store
	registry           *connectors.Registry
	adapterRegistry    *connectorapi.Registry
	connectorResources connectorruntime.ResourceScopes
	consoleSessions    *console.Manager
	commandRequests    *commandrequests.Runtime
	fileTransfers      *filetransferhttp.Runtime
	transferLifecycle  *filetransferhttp.Lifecycle
	securityPolicy     *securitypolicy.Service
	actionWorkflowMu   sync.Mutex
	actionWorkflow     *actions.Runtime
	projectVaultMu     sync.Mutex
	projectVault       *projectvault.Runtime
	runtimeState       runtimecontrol.State
	workspaceUUID      string
	uiRetryIdentity    string
	runtimeInstanceID  string
	actionIdentityKey  []byte
	vaultLeases        *vaultsessions.Store
	vaultDelivery      vaultDeliveryCoordinator
	identityMu         sync.Mutex
	auditDispatcher    *observability.Dispatcher
	retention          *retention.Service
	databaseOwnership  *dbpkg.DatabaseOwnership
}

type serverOptions struct {
	registry                   *connectors.Registry
	adapterRegistry            *connectorapi.Registry
	maintenanceConsole         console.MaintenanceConsoleRuntime
	runtimeInstanceIDGenerator func() (string, error)
}

type ServerOption func(*serverOptions)

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return func(options *serverOptions) {
		options.registry = registry
	}
}

func WithConnectorAdapterRegistry(registry *connectorapi.Registry) ServerOption {
	return func(options *serverOptions) {
		options.adapterRegistry = registry
	}
}

func WithMaintenanceConsole(runtime console.MaintenanceConsoleRuntime) ServerOption {
	return func(options *serverOptions) {
		options.maintenanceConsole = runtime
	}
}

func withRuntimeInstanceIDGenerator(generator func() (string, error)) ServerOption {
	return func(options *serverOptions) {
		options.runtimeInstanceIDGenerator = generator
	}
}

func resolveServerOptions(options []ServerOption) serverOptions {
	resolved := serverOptions{
		registry:                   connectors.NewRegistry(),
		adapterRegistry:            connectorapi.NewRegistry(),
		runtimeInstanceIDGenerator: executionprincipal.NewRuntimeInstanceID,
	}
	for _, option := range options {
		if option != nil {
			option(&resolved)
		}
	}
	if resolved.registry == nil {
		resolved.registry = connectors.NewRegistry()
	}
	if resolved.adapterRegistry == nil {
		resolved.adapterRegistry = connectorapi.NewRegistry()
	}
	if resolved.runtimeInstanceIDGenerator == nil {
		resolved.runtimeInstanceIDGenerator = executionprincipal.NewRuntimeInstanceID
	}
	return resolved
}

func NewServer(configuration RuntimeConfiguration, database *sql.DB, secretVault *vault.Vault, tokenStore *tokens.Store, options ...ServerOption) (*Server, error) {
	cfg := snapshotRuntimeConfiguration(configuration)
	databasecatalog.ScavengeTempPaths(cfg.DataPath, time.Now())
	activeID := databasecatalog.DefaultDatabaseID(cfg.DataPath)
	resolved := resolveServerOptions(options)
	registry := resolved.registry
	server := &Server{
		config:               cfg,
		workspaces:           workspacelifecycle.NewRegistry(cfg.DataPath, activeID, describeDatabaseRuntime),
		registry:             registry,
		adapterRegistry:      resolved.adapterRegistry,
		mux:                  http.NewServeMux(),
		maintenanceConsole:   resolved.maintenanceConsole,
		authLimiter:          runtimecontrol.NewAuth(1, authRateLimitLockoutFailures),
		mcpIPAuthLimiter:     runtimecontrol.NewAuth(mcpGlobalDelayFailures, mcpGlobalLockoutFailures),
		mcpTokenAuthLimiter:  runtimecontrol.NewAuth(1, authRateLimitLockoutFailures),
		vaultRevealLimiter:   runtimecontrol.NewWindow(8, time.Minute),
		vaultGenerateLimiter: runtimecontrol.NewWindow(10, time.Minute),
		vaultRequestLimiter:  runtimecontrol.NewWindow(30, time.Minute),
		uiSessions:           uisession.New(cfg.FrontendPort),
	}
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		return nil, err
	}
	runtime := &databaseRuntime{
		id:              activeID,
		path:            cfg.DataPath,
		gatewaySecret:   cfg.GatewaySecret,
		database:        database,
		vault:           secretVault,
		tokens:          tokenStore,
		registry:        registry,
		adapterRegistry: resolved.adapterRegistry,
		securityPolicy:  securitypolicy.NewService(database),
		vaultLeases:     vaultsessions.NewStore(),
	}
	runtime.transferLifecycle = filetransferhttp.NewLifecycle()
	var err error
	runtime.workspaceUUID, err = projectvault.EnsureWorkspaceUUID(context.Background(), database)
	if err != nil {
		return nil, fmt.Errorf("initialize workspace identity: %w", err)
	}
	runtime.uiRetryIdentity, err = projectvault.EnsureUIRetryIdentity(context.Background(), database)
	if err != nil {
		return nil, fmt.Errorf("initialize UI retry identity: %w", err)
	}
	runtime.actionIdentityKey, err = actions.DeriveIdentityKey(cfg.GatewaySecret, runtime.workspaceUUID)
	if err != nil {
		return nil, fmt.Errorf("initialize connector action identity: %w", err)
	}
	runtime.connectorResources = connectorruntime.NewResourceScopes(database, secretVault, runtime.workspaceUUID)
	runtime.runtimeInstanceID, err = resolved.runtimeInstanceIDGenerator()
	if err != nil {
		return nil, fmt.Errorf("initialize runtime identity: %w", err)
	}
	runtime.consoleSessions = console.NewManager(database, server.runtimeConsoleOpener(runtime), server.runtimeRedactor(runtime))
	if err := server.initializeCommandRequestRuntime(runtime); err != nil {
		return nil, fmt.Errorf("initialize command request runtime: %w", err)
	}
	if err := server.initializeFileTransferRuntime(runtime); err != nil {
		return nil, fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := server.configureVaultSessionRuntime(runtime); err != nil {
		runtime.transferLifecycle.Stop()
		return nil, fmt.Errorf("initialize Vault session runtime: %w", err)
	}
	server.configureAuditDispatcher(runtime)
	server.workspaces.Activate(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}

func NewLockedServer(configuration RuntimeConfiguration, options ...ServerOption) *Server {
	cfg := snapshotRuntimeConfiguration(configuration)
	databasecatalog.ScavengeTempPaths(cfg.DataPath, time.Now())
	resolved := resolveServerOptions(options)
	server := &Server{
		config:               cfg,
		workspaces:           workspacelifecycle.NewRegistry(cfg.DataPath, databasecatalog.DefaultDatabaseID(cfg.DataPath), describeDatabaseRuntime),
		registry:             resolved.registry,
		adapterRegistry:      resolved.adapterRegistry,
		mux:                  http.NewServeMux(),
		maintenanceConsole:   resolved.maintenanceConsole,
		authLimiter:          runtimecontrol.NewAuth(1, authRateLimitLockoutFailures),
		mcpIPAuthLimiter:     runtimecontrol.NewAuth(mcpGlobalDelayFailures, mcpGlobalLockoutFailures),
		mcpTokenAuthLimiter:  runtimecontrol.NewAuth(1, authRateLimitLockoutFailures),
		vaultRevealLimiter:   runtimecontrol.NewWindow(8, time.Minute),
		vaultGenerateLimiter: runtimecontrol.NewWindow(10, time.Minute),
		vaultRequestLimiter:  runtimecontrol.NewWindow(30, time.Minute),
		uiSessions:           uisession.New(cfg.FrontendPort),
	}
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		panic(fmt.Sprintf("initialize workspace lifecycle: %v", err))
	}
	server.routes()
	return server
}

func (runtime *databaseRuntime) WorkspaceIdentity() workspacelifecycle.Identity {
	return describeDatabaseRuntime(runtime)
}

func (runtime *databaseRuntime) WorkspaceDatabase() *sql.DB { return runtime.database }

func (runtime *databaseRuntime) WorkspaceGatewaySecret() string { return runtime.gatewaySecret }

func (s *Server) initializeWorkspaceLifecycle() error {
	lifecycle, err := workspacelifecycle.NewService(workspacelifecycle.Dependencies[*databaseRuntime]{
		DataPath: s.config.DataPath,
		Registry: s.workspaces,
		Open:     s.openRuntimeForLifecycle,
		Close:    s.closeRuntime,
		OnActivated: func(runtime *databaseRuntime) {
			if runtime != nil && runtime.gatewaySecret != "" {
				s.config.GatewaySecret = runtime.gatewaySecret
			}
		},
		OnOpened: s.initializeRetention,
	})
	if err != nil {
		return fmt.Errorf("initialize workspace lifecycle: %w", err)
	}
	s.workspaceLifecycle = lifecycle
	return nil
}

func describeDatabaseRuntime(runtime *databaseRuntime) workspacelifecycle.Identity {
	if runtime == nil {
		return workspacelifecycle.Identity{}
	}
	return workspacelifecycle.Identity{
		ID: runtime.id, Path: runtime.path, RetryIdentity: runtime.uiRetryIdentity,
	}
}

func (s *Server) connectorRegistry() *connectors.Registry {
	if s != nil && s.registry != nil {
		return s.registry
	}
	return connectors.NewRegistry()
}

func (s *Server) connectorAdapterRegistry() *connectorapi.Registry {
	if s != nil && s.adapterRegistry != nil {
		return s.adapterRegistry
	}
	return connectorapi.NewRegistry()
}

func (runtime *databaseRuntime) connectorRegistry() *connectors.Registry {
	if runtime != nil && runtime.registry != nil {
		return runtime.registry
	}
	return connectors.NewRegistry()
}

func (runtime *databaseRuntime) connectorAdapterRegistry() *connectorapi.Registry {
	if runtime != nil && runtime.adapterRegistry != nil {
		return runtime.adapterRegistry
	}
	return connectorapi.NewRegistry()
}
