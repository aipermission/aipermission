package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"github.com/aipermission/aipermission/backend/internal/applicationobservation"
	"github.com/aipermission/aipermission/backend/internal/connectorapi"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/gatewayoptions"
	"github.com/aipermission/aipermission/backend/internal/gatewaystate"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/workspaceruntime"
)

type Server struct {
	config         serverConfig
	workspaceState gatewaystate.WorkspaceState
	connectorState gatewaystate.ConnectorState
	controlState   gatewaystate.ControlState
	mux            *http.ServeMux
	observation    applicationobservation.Component
}

type databaseRuntime = workspaceruntime.Runtime

type ServerOption = gatewayoptions.Option

func WithConnectorRegistry(registry *connectors.Registry) ServerOption {
	return gatewayoptions.WithConnectorRegistry(registry)
}

func WithConnectorAdapterRegistry(registry *connectorapi.Registry) ServerOption {
	return gatewayoptions.WithConnectorAdapterRegistry(registry)
}

func WithMaintenanceConsole(runtime console.MaintenanceConsoleRuntime) ServerOption {
	return gatewayoptions.WithMaintenanceConsole(runtime)
}

func withRuntimeInstanceIDGenerator(generator func() (string, error)) ServerOption {
	return gatewayoptions.WithRuntimeInstanceIDGenerator(generator)
}

func NewServer(configuration RuntimeConfiguration, database *sql.DB, secretVault *gatewayworkspace.Vault, tokenStore *gatewayworkspace.TokenStore, options ...ServerOption) (*Server, error) {
	cfg := snapshotRuntimeConfiguration(configuration)
	gatewayworkspace.Scavenge(cfg.DataPath, time.Now())
	activeID := gatewayworkspace.DefaultID(cfg.DataPath)
	resolved := gatewayoptions.Resolve(options)
	registry := resolved.Registry
	server := &Server{
		config: cfg,
		workspaceState: gatewaystate.WorkspaceState{
			Registry: gatewayworkspace.NewRegistry(cfg.DataPath, activeID, describeDatabaseRuntime),
		},
		connectorState: gatewaystate.NewConnectorState(registry, resolved.AdapterRegistry),
		controlState:   gatewaystate.NewControlState(cfg.FrontendPort, resolved.MaintenanceConsole),
		mux:            http.NewServeMux(),
	}
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		return nil, err
	}
	runtime, err := gatewayworkspace.Adopt(context.Background(), gatewayworkspace.AdoptInput{
		ID: activeID, Path: cfg.DataPath, Database: database, Vault: secretVault,
		TokenStore: tokenStore, ConfiguredGatewaySecret: cfg.GatewaySecret,
		Registry: registry, AdapterRegistry: resolved.AdapterRegistry,
		RuntimeInstanceID: resolved.RuntimeInstanceIDGenerator,
	})
	if err != nil {
		return nil, err
	}
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
	server.workspaceState.Registry.Activate(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}

func NewLockedServer(configuration RuntimeConfiguration, options ...ServerOption) *Server {
	cfg := snapshotRuntimeConfiguration(configuration)
	gatewayworkspace.Scavenge(cfg.DataPath, time.Now())
	resolved := gatewayoptions.Resolve(options)
	server := &Server{
		config: cfg,
		workspaceState: gatewaystate.WorkspaceState{
			Registry: gatewayworkspace.NewRegistry(cfg.DataPath, gatewayworkspace.DefaultID(cfg.DataPath), describeDatabaseRuntime),
		},
		connectorState: gatewaystate.NewConnectorState(resolved.Registry, resolved.AdapterRegistry),
		controlState:   gatewaystate.NewControlState(cfg.FrontendPort, resolved.MaintenanceConsole),
		mux:            http.NewServeMux(),
	}
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		panic(fmt.Sprintf("initialize workspace lifecycle: %v", err))
	}
	server.routes()
	return server
}

func (s *Server) initializeWorkspaceLifecycle() error {
	lifecycle, err := gatewayworkspace.NewService(gatewayworkspace.Dependencies{
		DataPath:      s.config.DataPath,
		Registry:      s.workspaceState.Registry,
		Open:          s.openRuntimeForLifecycle,
		Close:         s.closeRuntime,
		Move:          s.moveDatabase,
		Delete:        gatewayworkspace.Delete,
		Publish:       s.publishDatabase,
		GatewaySecret: func() string { return s.config.GatewaySecret },
		OnActivated: func(runtime *databaseRuntime) {
			if runtime != nil && runtime.GatewaySecret != "" {
				s.config.GatewaySecret = runtime.GatewaySecret
			}
		},
		OnOpened: s.initializeRetention,
		ValidateNewPassword: func(ctx context.Context, database *sql.DB, databaseName, password string) error {
			hasActiveRemoteBackup, err := gatewayworkspace.HasActiveRemoteBackup(ctx, database)
			if err != nil || !hasActiveRemoteBackup {
				return err
			}
			if err := gatewayworkspace.ValidateRemoteBackupPassword(password, databaseName); err != nil {
				return gatewayworkspace.PasswordPolicyError(err)
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

func describeDatabaseRuntime(runtime *databaseRuntime) gatewayworkspace.Identity {
	if runtime == nil {
		return gatewayworkspace.Identity{}
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
