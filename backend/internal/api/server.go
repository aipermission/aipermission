package api

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewayoperations "github.com/aipermission/aipermission/backend/internal/gatewayoperations"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

type Server struct {
	config                   serverConfig
	access                   *gatewayaccess.Component
	connectorActions         *gatewayinfra.ConnectorActionApplication
	connectorRuntime         *gatewayinfra.ConnectorRuntimeApplication
	connectorManagement      *gatewayinfra.ConnectorManagementApplication
	vault                    *gatewayvault.Component
	workspaceOwner           *gatewayinfra.WorkspaceOwner
	accessOwner              *gatewayinfra.AccessOwner
	connectorActionOwner     *gatewayinfra.ConnectorActionOwner
	connectorManagementOwner *gatewayinfra.ConnectorManagementOwner
	connectorPortsOwner      *gatewayinfra.ConnectorPortsOwner
	observationOwner         *gatewayinfra.ObservationOwner
	operationsOwner          *gatewayinfra.OperationsOwner
	vaultOwner               *gatewayinfra.VaultOwner
	mux                      *http.ServeMux
	commands                 gatewayoperations.CommandComponent
	transfers                *gatewaytransfer.Component
	connectorRegistryOwner   *connectors.Registry
	connectorAdaptersOwner   *connectorapi.Registry
	maintenanceConsole       gatewayoperations.MaintenanceConsoleRuntime
	runtimeIDGenerator       func() (string, error)
	openRuntimeOverride      func(context.Context, string, string, string) (*gatewayinfra.WorkspaceHandle, error)
	moveDatabaseOverride     func(string, string) error
	publishDatabaseOverride  func(string, string) error
}

func NewLockedServer(configuration RuntimeConfiguration, options ...ServerOption) *Server {
	cfg := snapshotRuntimeConfiguration(configuration)
	resolved := resolveServerOptions(options)
	infrastructure := gatewayinfra.NewComponent(cfg.DataPath, describeDatabaseRuntime)
	server := newServerComposition(cfg, resolved, infrastructure)
	if err := server.initializeWorkspaceLifecycle(); err != nil {
		panic(fmt.Sprintf("initialize workspace lifecycle: %v", err))
	}
	server.routes()
	return server
}

func newServerComposition(cfg serverConfig, resolved serverOptions, infrastructure *gatewayinfra.Component) *Server {
	server := &Server{
		config: cfg, access: gatewayaccess.NewComponent(cfg.FrontendPort),
		transfers: gatewaytransfer.NewComponent(), mux: http.NewServeMux(), connectorRegistryOwner: resolved.registry,
		connectorAdaptersOwner: resolved.adapterRegistry, maintenanceConsole: resolved.maintenanceConsole,
		runtimeIDGenerator: resolved.runtimeInstanceIDGenerator,
	}
	server.bindInfrastructure(infrastructure)
	server.connectorRuntime = server.newConnectorRuntimeApplication()
	server.connectorManagement = server.newConnectorManagementApplication()
	server.vault = server.newVaultApplication()
	if err := server.configureConnectorActionApplication(); err != nil {
		panic(fmt.Sprintf("initialize connector action application: %v", err))
	}
	return server
}

func (s *Server) bindInfrastructure(infrastructure *gatewayinfra.Component) {
	s.workspaceOwner = infrastructure.WorkspaceOwner()
	s.accessOwner = infrastructure.AccessOwner()
	s.connectorActionOwner = infrastructure.ConnectorActionOwner()
	s.connectorManagementOwner = infrastructure.ConnectorManagementOwner()
	s.connectorPortsOwner = infrastructure.ConnectorPortsOwner()
	s.observationOwner = infrastructure.ObservationOwner()
	s.operationsOwner = infrastructure.OperationsOwner()
	s.vaultOwner = infrastructure.VaultOwner()
}

func (s *Server) initializeWorkspaceLifecycle() error {
	err := s.workspaceOwner.ConfigureWorkspaceLifecycle(gatewayinfra.WorkspaceDependencies{
		DataPath:      s.config.DataPath,
		Open:          s.openRuntimeForLifecycle,
		Close:         s.closeRuntime,
		Move:          s.moveDatabase,
		Delete:        s.workspaceOwner.DeleteDatabase,
		Publish:       s.publishDatabase,
		GatewaySecret: func() string { return s.config.GatewaySecret },
		OnActivated: func(runtime *gatewayinfra.WorkspaceHandle) {
			if secret := s.workspaceOwner.ConfiguredGatewaySecret(runtime); secret != "" {
				s.config.GatewaySecret = secret
			}
		},
		OnOpened: s.initializeRetention,
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
