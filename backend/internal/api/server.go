package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/applicationobservation"
	"github.com/aipermission/aipermission/backend/internal/backups"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/uisession"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/workspacelifecycle"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime/foundation"
)

type Server struct {
	config               serverConfig
	workspaces           *workspacelifecycle.Registry[*databaseRuntime]
	workspaceLifecycle   *workspacelifecycle.Service[*databaseRuntime]
	registry             *connectors.Registry
	adapterRegistry      *connectorapi.Registry
	mux                  *http.ServeMux
	maintenanceConsole   console.MaintenanceConsoleRuntime
	authLimiter          *runtimecontrol.Auth
	mcpIPAuthLimiter     *runtimecontrol.Auth
	mcpTokenAuthLimiter  *runtimecontrol.Auth
	vaultRevealLimiter   *runtimecontrol.Window
	vaultGenerateLimiter *runtimecontrol.Window
	vaultRequestLimiter  *runtimecontrol.Window
	uiSessions           *uisession.Manager
	observation          applicationobservation.Component
	databaseMove         func(string, string) error
	databasePublish      func(string, string) error
	runtimeOpen          func(string, string, string) (*databaseRuntime, error)
	backupOperations     backups.OperationLimiter
}

type databaseRuntime = workspaceruntime.Runtime

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
	foundationState, err := foundation.Adopt(context.Background(), foundation.AdoptInput{
		ID: activeID, Path: cfg.DataPath, Database: database, Vault: secretVault,
		TokenStore: tokenStore, ConfiguredGatewaySecret: cfg.GatewaySecret,
		Registry: registry, AdapterRegistry: resolved.adapterRegistry,
		RuntimeInstanceID: resolved.runtimeInstanceIDGenerator,
	})
	if err != nil {
		return nil, err
	}
	runtime := workspaceruntime.New(foundationState)
	runtime.Connectors.ConsoleSessions = console.NewManager(database, server.runtimeConsoleOpener(runtime), server.runtimeRedactor(runtime))
	if err := server.initializeCommandRequestRuntime(runtime); err != nil {
		return nil, fmt.Errorf("initialize command request runtime: %w", err)
	}
	if err := server.initializeFileTransferRuntime(runtime); err != nil {
		return nil, fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := server.configureVaultSessionRuntime(runtime); err != nil {
		runtime.Operations.TransferLifecycle.Stop()
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

func (s *Server) initializeWorkspaceLifecycle() error {
	lifecycle, err := workspacelifecycle.NewService(workspacelifecycle.Dependencies[*databaseRuntime]{
		DataPath:      s.config.DataPath,
		Registry:      s.workspaces,
		Open:          s.openRuntimeForLifecycle,
		Close:         s.closeRuntime,
		Move:          s.moveDatabase,
		Delete:        databasecatalog.DeleteDatabase,
		Publish:       s.publishDatabase,
		GatewaySecret: func() string { return s.config.GatewaySecret },
		OnActivated: func(runtime *databaseRuntime) {
			if runtime != nil && runtime.GatewaySecret != "" {
				s.config.GatewaySecret = runtime.GatewaySecret
			}
		},
		OnOpened: s.initializeRetention,
		ValidateNewPassword: func(ctx context.Context, database *sql.DB, databaseName, password string) error {
			hasActiveRemoteBackup, err := backups.NewStore(database).HasActiveProvider(ctx)
			if err != nil || !hasActiveRemoteBackup {
				return err
			}
			if err := backups.ValidateRemoteBackupPassword(password, databaseName); err != nil {
				return workspacelifecycle.PasswordPolicyError(err)
			}
			return nil
		},
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
	return runtime.WorkspaceIdentity()
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

func runtimeConnectorRegistry(runtime *databaseRuntime) *connectors.Registry {
	return runtime.Connectors.ConnectorRegistry()
}

func runtimeConnectorAdapterRegistry(runtime *databaseRuntime) *connectorapi.Registry {
	return runtime.Connectors.ConnectorAdapterRegistry()
}
