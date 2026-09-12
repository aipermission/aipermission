package gatewayinfrastructure

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/gatewayvault"
)

func completeVaultHTTPDependencies() VaultHTTPDependencies {
	return VaultHTTPDependencies{
		Active:       func(http.ResponseWriter) (*WorkspaceHandle, bool) { return nil, false },
		RuntimePorts: func(*WorkspaceHandle) VaultRuntimePorts { return VaultRuntimePorts{} },
		ProjectPorts: func(*WorkspaceHandle) ProjectPorts { return ProjectPorts{} },
		MCP: func(http.ResponseWriter, *http.Request) (*WorkspaceHandle, VaultMCPHTTPPorts, bool) {
			return nil, VaultMCPHTTPPorts{}, false
		},
	}
}

func TestVaultHTTPHandlersOwnCompleteTransportSet(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	handlers := component.VaultOwner().HTTPHandlers(gatewayvault.New(gatewayvault.Dependencies{}), completeVaultHTTPDependencies())
	if handlers.Projects == nil || handlers.ProjectVault == nil || handlers.VaultApprovals == nil || handlers.MCPVault == nil {
		t.Fatal("Vault owner returned an incomplete HTTP handler set")
	}

	tests := []struct {
		name string
		call func(http.ResponseWriter, *http.Request)
	}{
		{name: "projects", call: handlers.Projects.List},
		{name: "project Vault", call: handlers.ProjectVault.ListItems},
		{name: "Vault approvals", call: handlers.VaultApprovals.List},
		{name: "MCP Vault", call: handlers.MCPVault.ListItems},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			test.call(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want untouched response when composition rejects access", response.Code)
			}
		})
	}
}

func TestVaultHTTPHandlersRejectIncompleteComposition(t *testing.T) {
	component := NewComponent(t.TempDir(), nil)
	application := gatewayvault.New(gatewayvault.Dependencies{})
	complete := completeVaultHTTPDependencies()

	tests := []struct {
		name   string
		owner  *VaultOwner
		app    *gatewayvault.Component
		mutate func(*VaultHTTPDependencies)
	}{
		{name: "nil owner", owner: nil, app: application},
		{name: "nil application", owner: component.VaultOwner(), app: nil},
		{name: "missing active resolver", owner: component.VaultOwner(), app: application, mutate: func(deps *VaultHTTPDependencies) { deps.Active = nil }},
		{name: "missing runtime ports", owner: component.VaultOwner(), app: application, mutate: func(deps *VaultHTTPDependencies) { deps.RuntimePorts = nil }},
		{name: "missing project ports", owner: component.VaultOwner(), app: application, mutate: func(deps *VaultHTTPDependencies) { deps.ProjectPorts = nil }},
		{name: "missing MCP authentication", owner: component.VaultOwner(), app: application, mutate: func(deps *VaultHTTPDependencies) { deps.MCP = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dependencies := complete
			if test.mutate != nil {
				test.mutate(&dependencies)
			}
			handlers := test.owner.HTTPHandlers(test.app, dependencies)
			if handlers.Projects != nil || handlers.ProjectVault != nil || handlers.VaultApprovals != nil || handlers.MCPVault != nil {
				t.Fatal("incomplete Vault composition exposed HTTP handlers")
			}
		})
	}
}
