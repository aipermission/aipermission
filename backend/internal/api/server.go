package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type Server struct {
	config                  serverConfig
	access                  *gatewayaccess.Component
	connectorActions        *gatewayactions.Component
	connectorPorts          *connectorapi.PortsComponent
	connectorManagement     *connectormgmt.Component
	vault                   *gatewayvault.Component
	infrastructure          *gatewayinfra.Component
	mux                     *http.ServeMux
	observation             gatewayoperations.Observation
	openRuntimeOverride     func(string, string, string) (databaseRuntime, error)
	moveDatabaseOverride    func(string, string) error
	publishDatabaseOverride func(string, string) error
}

type databaseRuntime = gatewayinfra.Runtime

type ServerOption = gatewayinfra.ServerOption

func WithConnectorRegistry(registry *connectorapi.ConnectorRegistry) ServerOption {
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
	infrastructure := gatewayinfra.NewComponent(cfg.DataPath, describeDatabaseRuntime, options...)
	registry := infrastructure.ConnectorRegistry()
	server := &Server{
		config: cfg, access: gatewayaccess.NewComponent(cfg.FrontendPort), infrastructure: infrastructure, mux: http.NewServeMux(),
	}
	server.connectorActions = server.newConnectorActionApplication()
	server.connectorPorts = server.newConnectorPortsApplication()
	server.connectorManagement = server.newConnectorManagementApplication()
	server.vault = server.newVaultApplication()
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		return nil, err
	}
	runtime, err := infrastructure.AdoptWorkspace(context.Background(), gatewayinfra.AdoptInput{
		ID: infrastructure.WorkspaceSelection().ID, Path: cfg.DataPath, Database: database, Vault: secretVault,
		TokenStore: tokenStore, ConfiguredGatewaySecret: cfg.GatewaySecret,
		Registry: registry, AdapterRegistry: infrastructure.ConnectorAdapterRegistry(),
		RuntimeInstanceID: infrastructure.RuntimeInstanceIDGenerator(),
	})
	if err != nil {
		return nil, err
	}
	runtime.ConnectorPort().SetConsoleSessionManager(gatewayoperations.NewConsoleManager(database, server.runtimeConsoleOpener(runtime), server.runtimeRedactor(runtime)))
	if err := server.initializeCommandRequestRuntime(runtime); err != nil {
		return nil, fmt.Errorf("initialize command request runtime: %w", err)
	}
	if err := server.initializeFileTransferRuntime(runtime); err != nil {
		return nil, fmt.Errorf("initialize file transfer runtime: %w", err)
	}
	if err := server.configureVaultSessionRuntime(runtime); err != nil {
		runtime.OperationsPort().FileTransferLifecycle().Stop()
		return nil, fmt.Errorf("initialize Vault session runtime: %w", err)
	}
	server.configureAuditDispatcher(runtime)
	server.infrastructure.ActivateWorkspace(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}

func NewLockedServer(configuration RuntimeConfiguration, options ...ServerOption) *Server {
	cfg := snapshotRuntimeConfiguration(configuration)
	infrastructure := gatewayinfra.NewComponent(cfg.DataPath, describeDatabaseRuntime, options...)
	server := &Server{
		config: cfg, access: gatewayaccess.NewComponent(cfg.FrontendPort), infrastructure: infrastructure, mux: http.NewServeMux(),
	}
	server.connectorActions = server.newConnectorActionApplication()
	server.connectorPorts = server.newConnectorPortsApplication()
	server.connectorManagement = server.newConnectorManagementApplication()
	server.vault = server.newVaultApplication()
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		panic(fmt.Sprintf("initialize workspace lifecycle: %v", err))
	}
	server.routes()
	return server
}

func (s *Server) initializeWorkspaceLifecycle() error {
	err := s.infrastructure.ConfigureWorkspaceLifecycle(gatewayinfra.WorkspaceDependencies{
		DataPath:      s.config.DataPath,
		Open:          s.openRuntimeForLifecycle,
		Close:         s.closeRuntime,
		Move:          s.moveDatabase,
		Delete:        s.infrastructure.DeleteDatabase,
		Publish:       s.publishDatabase,
		GatewaySecret: func() string { return s.config.GatewaySecret },
		OnActivated: func(runtime databaseRuntime) {
			if runtime != nil && runtime.GatewaySecretValue() != "" {
				s.config.GatewaySecret = runtime.GatewaySecretValue()
			}
		},
		OnOpened: s.initializeRetention,
		ValidateNewPassword: func(ctx context.Context, database *sql.DB, databaseName, password string) error {
			hasActiveRemoteBackup, err := s.infrastructure.HasActiveRemoteBackup(ctx, database)
			if err != nil || !hasActiveRemoteBackup {
				return err
			}
			if err := s.infrastructure.ValidateRemoteBackupPassword(password, databaseName); err != nil {
				return s.infrastructure.PasswordPolicyError(err)
			}
			return nil
		},
	})
	if err != nil {
		return fmt.Errorf("initialize workspace lifecycle: %w", err)
	}
	return nil
}

func describeDatabaseRuntime(runtime databaseRuntime) gatewayinfra.Identity {
	if runtime == nil {
		return gatewayinfra.Identity{}
	}
	return runtime.WorkspaceIdentity()
}

func (s *Server) connectorRegistry() *connectorapi.ConnectorRegistry {
	if s != nil && s.infrastructure != nil && s.infrastructure.ConnectorRegistry() != nil {
		return s.infrastructure.ConnectorRegistry()
	}
	return connectorapi.NewConnectorRegistry()
}

func (s *Server) connectorAdapterRegistry() *connectorapi.Registry {
	if s != nil && s.infrastructure != nil && s.infrastructure.ConnectorAdapterRegistry() != nil {
		return s.infrastructure.ConnectorAdapterRegistry()
	}
	return connectorapi.NewRegistry()
}

func runtimeConnectorRegistry(runtime databaseRuntime) *connectorapi.ConnectorRegistry {
	return runtime.ConnectorPort().ConnectorRegistry()
}

func runtimeConnectorAdapterRegistry(runtime databaseRuntime) *connectorapi.Registry {
	return runtime.ConnectorPort().ConnectorAdapterRegistry()
}
