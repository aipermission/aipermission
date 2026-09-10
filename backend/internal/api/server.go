package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectors "github.com/aipermission/aipermission/backend/internal/gatewayconnectors"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
)

type Server struct {
	config         serverConfig
	workspaceState gatewayinfra.WorkspaceState
	connectorState gatewayinfra.ConnectorState
	controlState   gatewayinfra.ControlState
	mux            *http.ServeMux
	observation    gatewayoperations.Observation
}

type databaseRuntime = gatewayinfra.Runtime

type ServerOption = gatewayinfra.ServerOption

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return gatewayinfra.WithConnectorRegistry(registry)
}

func WithConnectorAdapterRegistry(registry *connectorapi.Registry) ServerOption {
	return gatewayinfra.WithConnectorAdapterRegistry(registry)
}

func WithMaintenanceConsole(runtime gatewayoperations.MaintenanceConsoleRuntime) ServerOption {
	return gatewayinfra.WithMaintenanceConsole(runtime)
}

func withRuntimeInstanceIDGenerator(generator func() (string, error)) ServerOption {
	return gatewayinfra.WithRuntimeInstanceIDGenerator(generator)
}

func NewServer(configuration RuntimeConfiguration, database *sql.DB, secretVault *gatewayinfra.Vault, tokenStore *gatewayinfra.TokenStore, options ...ServerOption) (*Server, error) {
	cfg := snapshotRuntimeConfiguration(configuration)
	gatewayinfra.Scavenge(cfg.DataPath, time.Now())
	activeID := gatewayinfra.DefaultID(cfg.DataPath)
	resolved := gatewayinfra.ResolveOptions(options)
	registry := resolved.Registry
	server := &Server{
		config: cfg,
		workspaceState: gatewayinfra.WorkspaceState{
			Registry: gatewayinfra.NewRegistry(cfg.DataPath, activeID, describeDatabaseRuntime),
		},
		connectorState: gatewayinfra.NewConnectorState(registry, resolved.AdapterRegistry),
		controlState:   gatewayinfra.NewControlState(cfg.FrontendPort, resolved.MaintenanceConsole),
		mux:            http.NewServeMux(),
	}
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		return nil, err
	}
	runtime, err := gatewayinfra.Adopt(context.Background(), gatewayinfra.AdoptInput{
		ID: activeID, Path: cfg.DataPath, Database: database, Vault: secretVault,
		TokenStore: tokenStore, ConfiguredGatewaySecret: cfg.GatewaySecret,
		Registry: registry, AdapterRegistry: resolved.AdapterRegistry,
		RuntimeInstanceID: resolved.RuntimeInstanceIDGenerator,
	})
	if err != nil {
		return nil, err
	}
	runtime.Connectors.ConsoleSessions = gatewayoperations.NewConsoleManager(database, server.runtimeConsoleOpener(runtime), server.runtimeRedactor(runtime))
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
	server.workspaceState.Registry.Activate(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}

func NewLockedServer(configuration RuntimeConfiguration, options ...ServerOption) *Server {
	cfg := snapshotRuntimeConfiguration(configuration)
	gatewayinfra.Scavenge(cfg.DataPath, time.Now())
	resolved := gatewayinfra.ResolveOptions(options)
	server := &Server{
		config: cfg,
		workspaceState: gatewayinfra.WorkspaceState{
			Registry: gatewayinfra.NewRegistry(cfg.DataPath, gatewayinfra.DefaultID(cfg.DataPath), describeDatabaseRuntime),
		},
		connectorState: gatewayinfra.NewConnectorState(resolved.Registry, resolved.AdapterRegistry),
		controlState:   gatewayinfra.NewControlState(cfg.FrontendPort, resolved.MaintenanceConsole),
		mux:            http.NewServeMux(),
	}
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		panic(fmt.Sprintf("initialize workspace lifecycle: %v", err))
	}
	server.routes()
	return server
}

func (s *Server) initializeWorkspaceLifecycle() error {
	lifecycle, err := gatewayinfra.NewService(gatewayinfra.WorkspaceDependencies{
		DataPath:      s.config.DataPath,
		Registry:      s.workspaceState.Registry,
		Open:          s.openRuntimeForLifecycle,
		Close:         s.closeRuntime,
		Move:          s.moveDatabase,
		Delete:        gatewayinfra.Delete,
		Publish:       s.publishDatabase,
		GatewaySecret: func() string { return s.config.GatewaySecret },
		OnActivated: func(runtime *databaseRuntime) {
			if runtime != nil && runtime.GatewaySecret != "" {
				s.config.GatewaySecret = runtime.GatewaySecret
			}
		},
		OnOpened: s.initializeRetention,
		ValidateNewPassword: func(ctx context.Context, database *sql.DB, databaseName, password string) error {
			hasActiveRemoteBackup, err := gatewayinfra.HasActiveRemoteBackup(ctx, database)
			if err != nil || !hasActiveRemoteBackup {
				return err
			}
			if err := gatewayinfra.ValidateRemoteBackupPassword(password, databaseName); err != nil {
				return gatewayinfra.PasswordPolicyError(err)
			}
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("initialize workspace lifecycle: %w", err)
	}
	s.workspaceState.Lifecycle = lifecycle
	return nil
}

func describeDatabaseRuntime(runtime *databaseRuntime) gatewayinfra.Identity {
	if runtime == nil {
		return gatewayinfra.Identity{}
	}
	return runtime.WorkspaceIdentity()
}

func (s *Server) connectorRegistry() *connectors.Registry {
	if s != nil && s.connectorState.Registry != nil {
		return s.connectorState.Registry
	}
	return connectors.NewRegistry()
}

func (s *Server) connectorAdapterRegistry() *connectorapi.Registry {
	if s != nil && s.connectorState.AdapterRegistry != nil {
		return s.connectorState.AdapterRegistry
	}
	return connectorapi.NewRegistry()
}

func runtimeConnectorRegistry(runtime *databaseRuntime) *connectors.Registry {
	return runtime.Connectors.ConnectorRegistry()
}

func runtimeConnectorAdapterRegistry(runtime *databaseRuntime) *connectorapi.Registry {
	return runtime.Connectors.ConnectorAdapterRegistry()
}
