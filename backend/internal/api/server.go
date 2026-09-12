package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	gatewayactions "github.com/aipermission/aipermission/backend/internal/gatewayconnectoractions"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewaybootstrap "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/bootstrap"
	connectorports "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure/connectorports"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type Server struct {
	config                  serverConfig
	access                  *gatewayaccess.Component
	connectorActions        *gatewayactions.Component
	connectorPorts          *connectorports.PortsComponent
	connectorManagement     *connectormgmt.Component
	vault                   *gatewayvault.Component
	infrastructure          *gatewayinfra.Component
	mux                     *http.ServeMux
	commands                gatewayoperations.CommandComponent
	transfers               *gatewaytransfer.Component
	connectorRegistryOwner  *connectors.Registry
	connectorAdaptersOwner  *connectorapi.Registry
	maintenanceConsole      gatewayoperations.MaintenanceConsoleRuntime
	runtimeIDGenerator      func() (string, error)
	openRuntimeOverride     func(string, string, string) (*gatewayinfra.WorkspaceHandle, error)
	moveDatabaseOverride    func(string, string) error
	publishDatabaseOverride func(string, string) error
}

func NewServer(configuration RuntimeConfiguration, adopted gatewaybootstrap.Adopt, options ...ServerOption) (*Server, error) {
	cfg := snapshotRuntimeConfiguration(configuration)
	resolved := resolveServerOptions(options)
	infrastructure := gatewayinfra.NewComponent(cfg.DataPath, describeDatabaseRuntime)
	registry := resolved.registry
	server := &Server{
		config: cfg, access: gatewayaccess.NewComponent(cfg.FrontendPort), infrastructure: infrastructure,
		transfers: gatewaytransfer.NewComponent(), mux: http.NewServeMux(), connectorRegistryOwner: registry,
		connectorAdaptersOwner: resolved.adapterRegistry, maintenanceConsole: resolved.maintenanceConsole,
		runtimeIDGenerator: resolved.runtimeInstanceIDGenerator,
	}
	server.connectorActions = server.newConnectorActionApplication()
	server.connectorPorts = server.newConnectorPortsApplication()
	server.connectorManagement = server.newConnectorManagementApplication()
	server.vault = server.newVaultApplication()
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		return nil, err
	}
	adopted.ID = infrastructure.WorkspaceSelection().ID
	adopted.Path = cfg.DataPath
	adopted.ConfiguredGatewaySecret = cfg.GatewaySecret
	adopted.Registry = registry
	adopted.AdapterRegistry = resolved.adapterRegistry
	adopted.RuntimeInstanceID = resolved.runtimeInstanceIDGenerator
	runtime, err := infrastructure.AdoptWorkspace(context.Background(), adopted)
	if err != nil {
		return nil, err
	}
	if err := server.initializeOpenedRuntime(context.Background(), runtime); err != nil {
		server.discardOpeningRuntime(runtime)
		return nil, err
	}
	server.infrastructure.ActivateWorkspace(runtime)
	server.initializeRetention(runtime)
	server.routes()
	return server, nil
}

func NewLockedServer(configuration RuntimeConfiguration, options ...ServerOption) *Server {
	cfg := snapshotRuntimeConfiguration(configuration)
	resolved := resolveServerOptions(options)
	infrastructure := gatewayinfra.NewComponent(cfg.DataPath, describeDatabaseRuntime)
	server := &Server{
		config: cfg, access: gatewayaccess.NewComponent(cfg.FrontendPort), infrastructure: infrastructure,
		transfers: gatewaytransfer.NewComponent(), mux: http.NewServeMux(), connectorRegistryOwner: resolved.registry,
		connectorAdaptersOwner: resolved.adapterRegistry, maintenanceConsole: resolved.maintenanceConsole,
		runtimeIDGenerator: resolved.runtimeInstanceIDGenerator,
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
		OnActivated: func(runtime *gatewayinfra.WorkspaceHandle) {
			if secret := s.infrastructure.ConfiguredGatewaySecret(runtime); secret != "" {
				s.config.GatewaySecret = secret
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

func describeDatabaseRuntime(runtime *gatewayinfra.WorkspaceHandle) gatewayinfra.Identity {
	if runtime == nil {
		return gatewayinfra.Identity{}
	}
	return runtime.WorkspaceIdentity()
}

func (s *Server) connectorRegistry() *connectors.Registry {
	if s == nil {
		return nil
	}
	return s.connectorRegistryOwner
}

func (s *Server) connectorAdapterRegistry() *connectorapi.Registry {
	if s == nil {
		return nil
	}
	return s.connectorAdaptersOwner
}
