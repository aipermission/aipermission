package gatewayvault

import "testing"

func TestHTTPHandlersOwnEveryVaultTransport(t *testing.T) {
	handlers := New(Dependencies{}).HTTPHandlers(HTTPDependencies{})
	if handlers.Projects == nil || handlers.ProjectVault == nil || handlers.VaultApprovals == nil || handlers.MCPVault == nil {
		t.Fatal("Vault HTTP composition returned an incomplete handler set")
	}
}
