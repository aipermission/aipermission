package api

import (
	"context"
	"database/sql"
	"sync"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/console"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
	gatewaytransfer "github.com/aipermission/aipermission/backend/internal/gatewayoperations/transfer"
	"github.com/aipermission/aipermission/backend/internal/gatewayworkspace"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/securitypolicy"
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
	server.connectorActions = server.newConnectorActionApplication()
	server.connectorPorts = server.newConnectorPortsApplication()
	server.connectorManagement = server.newConnectorManagementApplication()
	server.vault = server.newVaultApplication()
	return server
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
	projection := server.vaultRuntime(runtime)
	store, ok := projection.Storage.Tokens.(*tokens.Store)
	if !ok || store == nil {
		t.Fatal("test runtime token store is unavailable")
	}
	return store
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

func testRuntimeControlState(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *runtimecontrol.State {
	t.Helper()
	projection, ok := server.accessOwner.MCPRuntimeScope(runtime, gatewayinfra.MCPRuntimePorts{
		StartEnabled: func(context.Context) (bool, error) { return false, nil },
	})
	if !ok || projection.State == nil {
		t.Fatal("test runtime control state is unavailable")
	}
	state, ok := projection.State.(*runtimecontrol.State)
	if !ok {
		t.Fatal("test runtime control state has an unexpected implementation")
	}
	return state
}

func testRuntimeSecurityPolicy(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle) *securitypolicy.Service {
	t.Helper()
	projection, ok := server.accessOwner.SecurityScope(runtime)
	if !ok || projection.Service == nil {
		t.Fatal("test runtime security policy is unavailable")
	}
	return projection.Service
}

func testMCPOutputAuthorization(t testing.TB, server *Server, runtime *gatewayinfra.WorkspaceHandle, tokenID int64) *gatewayaccess.MCPOutputAuthorization {
	t.Helper()
	scope, ok := server.accessOwner.MCPActionScope(runtime, gatewayinfra.MCPActionPorts{
		TokenID: tokenID, RunningHint: server.connectorRunningHint,
		Delivery:  server.connectorActionApplication().Delivery,
		Principal: func(id int64) (gatewayaccess.Principal, error) { return server.tokenExecutionPrincipal(runtime, id) },
		Call: func(context.Context, gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error) {
			return gatewayaccess.MCPActionCallResult{}, nil
		},
		Observe: func(context.Context, string, any) {},
		Redact:  func(_ context.Context, value string) string { return value },
	})
	if !ok || scope.Output == nil {
		t.Fatal("test MCP output authorization is unavailable")
	}
	return scope.Output
}
