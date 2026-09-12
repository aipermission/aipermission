package api

import (
	"database/sql"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	connectormgmt "github.com/aipermission/aipermission/backend/internal/gatewayconnectormanagement"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	gatewayvault "github.com/aipermission/aipermission/backend/internal/gatewayvault"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type runtimeTestOwner struct {
	workspaceOwner           *gatewayinfra.WorkspaceOwner
	accessOwner              *gatewayinfra.AccessOwner
	connectorActionOwner     *gatewayinfra.ConnectorActionOwner
	connectorManagementOwner *gatewayinfra.ConnectorManagementOwner
	connectorPortsOwner      *gatewayinfra.ConnectorPortsOwner
	observationOwner         *gatewayinfra.ObservationOwner
	operationsOwner          *gatewayinfra.OperationsOwner
	vaultOwner               *gatewayinfra.VaultOwner
	database                 *sql.DB
	secretVault              *vault.Vault
	tokens                   *tokens.Store
	registry                 *connectors.Registry
	adapters                 *connectorapi.Registry
}

var runtimeTestOwners sync.Map

func testAdoptInput(database *sql.DB, secretVault *vault.Vault, tokenStore *tokens.Store) gatewayworkspace.AdoptInput {
	return gatewayworkspace.AdoptInput{Database: database, Vault: secretVault, TokenStore: tokenStore}
}

func registerRuntimeTestOwner(runtime *gatewayinfra.WorkspaceHandle, owner runtimeTestOwner) {
	if runtime != nil {
		runtimeTestOwners.Store(runtime, owner)
	}
}

func requireRuntimeTestOwner(t testing.TB, runtime *gatewayinfra.WorkspaceHandle) runtimeTestOwner {
	t.Helper()
	owner, ok := runtimeTestOwners.Load(runtime)
	if !ok {
		t.Fatal("test runtime owner is unavailable")
	}
	return owner.(runtimeTestOwner)
}

func testServerForRuntime(t testing.TB, runtime *gatewayinfra.WorkspaceHandle) *Server {
	t.Helper()
	owner := requireRuntimeTestOwner(t, runtime)
	server := &Server{
		workspaceOwner: owner.workspaceOwner, accessOwner: owner.accessOwner,
		connectorActionOwner: owner.connectorActionOwner, connectorManagementOwner: owner.connectorManagementOwner,
		connectorPortsOwner: owner.connectorPortsOwner, observationOwner: owner.observationOwner,
		operationsOwner: owner.operationsOwner, vaultOwner: owner.vaultOwner,
		access:                 gatewayaccess.NewComponent("3001"),
		connectorRegistryOwner: owner.registry, connectorAdaptersOwner: owner.adapters,
		transfers: gatewaytransfer.NewComponent(),
	}
	server.connectorRuntime = server.newConnectorRuntimeApplication()
	server.connectorManagement = server.newConnectorManagementApplication()
	server.vault = server.newVaultApplication()
	if err := server.configureConnectorActionApplication(); err != nil {
		t.Fatal(err)
	}
	return server
}

func (s *Server) connectorCredentialPreparationPorts(runtime *gatewayinfra.WorkspaceHandle) connectormgmt.CredentialPreparationPorts {
	return s.connectorManagement.CredentialPreparation(runtime)
}

func (s *Server) vaultRuntime(runtime *gatewayinfra.WorkspaceHandle) gatewayvault.Runtime {
	if runtime == nil {
		return gatewayvault.Runtime{}
	}
	composed, ok := s.vaultOwner.VaultRuntime(runtime, s.vaultRuntimePorts(runtime))
	if !ok {
		return gatewayvault.Runtime{}
	}
	return composed
}

func testRuntimeDatabase(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *sql.DB {
	t.Helper()
	if server == nil || server.workspaceOwner == nil {
		t.Fatal("test runtime infrastructure is unavailable")
	}
	projection := server.vaultRuntime(runtime)
	if projection.Storage.Database == nil {
		t.Fatal("test runtime database is unavailable")
	}
	return projection.Storage.Database
}

func testRuntimeVault(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *vault.Vault {
	t.Helper()
	projection := server.vaultRuntime(runtime)
	if projection.Storage.SecretVault == nil {
		t.Fatal("test runtime Vault is unavailable")
	}
	return projection.Storage.SecretVault
}

func testRuntimeTokens(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *tokens.Store {
	t.Helper()
	identity := runtime.Identity()
	return tokens.NewEncryptedStore(
		testRuntimeDatabase(t, server, runtime),
		testRuntimeVault(t, server, runtime),
		identity.WorkspaceID,
	)
}

func testRuntimeLeases(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *vaultsessions.Store {
	t.Helper()
	projection := server.vaultRuntime(runtime)
	store, ok := projection.Session.Leases.(*vaultsessions.Store)
	if !ok || store == nil {
		t.Fatal("test runtime lease store is unavailable")
	}
	return store
}

func testRuntimeConsoleSessions(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *console.Manager {
	t.Helper()
	projection := server.vaultRuntime(runtime)
	manager, ok := projection.Session.Sessions.(*console.Manager)
	if !ok || manager == nil {
		t.Fatal("test runtime console manager is unavailable")
	}
	return manager
}

type testRuntimeControl struct {
	t       testing.TB
	server  *Server
	runtime *gatewayinfra.WorkspaceHandle
}

func (control testRuntimeControl) MCPStarted() bool {
	control.t.Helper()
	enabled, ok := control.server.accessOwner.MCPStarted(control.runtime)
	if !ok {
		control.t.Fatal("test runtime control state is unavailable")
	}
	return enabled
}

func (control testRuntimeControl) SetMCPStarted(enabled bool) {
	control.t.Helper()
	if !control.server.accessOwner.SetMCPStarted(control.runtime, enabled) {
		control.t.Fatal("test runtime control state is unavailable")
	}
}

func testRuntimeControlState(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) testRuntimeControl {
	t.Helper()
	if _, ok := server.accessOwner.MCPStarted(runtime); !ok {
		t.Fatal("test runtime control state is unavailable")
	}
	return testRuntimeControl{t: t, server: server, runtime: runtime}
}

func testMCPOutputAuthorization(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle, tokenID int64) *gatewayaccess.MCPOutputAuthorization {
	t.Helper()
	resources := requireRuntimeTestOwner(t, runtime)
	projection := server.vaultRuntime(runtime)
	if projection.Session.AcquireDelivery == nil || projection.Session.MCPStarted == nil {
		t.Fatal("test MCP output authorization is unavailable")
	}
	return &gatewayaccess.MCPOutputAuthorization{
		Database: resources.database, Tokens: resources.tokens, Leases: testRuntimeLeases(t, server, runtime),
		Delivery:   server.connectorActions.Delivery(projection.Session.AcquireDelivery),
		MCPStarted: projection.Session.MCPStarted,
		Principal:  func(id int64) (gatewayaccess.Principal, error) { return server.tokenExecutionPrincipal(runtime, id) },
	}
}
