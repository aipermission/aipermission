package gatewayvault

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPHandlersOwnEveryVaultTransport(t *testing.T) {
	handlers := New(Dependencies{}).HTTPHandlers(HTTPDependencies{})
	if handlers.Projects == nil || handlers.ProjectVault == nil || handlers.VaultApprovals == nil || handlers.MCPVault == nil {
		t.Fatal("Vault HTTP composition returned an incomplete handler set")
	}
}

func TestHTTPBoundaryScopeAdaptersPreserveAvailabilityAndCapabilities(t *testing.T) {
	if projectScopeProvider(nil) != nil || projectVaultScopeProvider(nil) != nil ||
		vaultApprovalScopeProvider(nil) != nil || mcpVaultScopeProvider(nil) != nil {
		t.Fatal("nil boundary providers must remain nil")
	}

	database := &sql.DB{}
	provider := projectScopeProvider(func(_ http.ResponseWriter) (ProjectScope, bool) {
		return ProjectScope{Database: database}, true
	})
	scope, ok := provider(httptest.NewRecorder())
	if !ok || scope.Database != database {
		t.Fatalf("project scope was not preserved: scope=%#v ok=%v", scope, ok)
	}

	approval := vaultApprovalScopeProvider(func(_ http.ResponseWriter) (VaultApprovalHTTPScope, bool) {
		return VaultApprovalHTTPScope{MCPStarted: func() bool { return true }}, true
	})
	approvalScope, ok := approval(httptest.NewRecorder())
	if !ok || approvalScope.MCPStarted == nil || !approvalScope.MCPStarted() || approvalScope.Runtime != nil {
		t.Fatalf("approval scope was not preserved: scope=%#v ok=%v", approvalScope, ok)
	}

	mcp := mcpVaultScopeProvider(func(_ http.ResponseWriter, _ *http.Request) (VaultMCPHTTPScope, bool) {
		return VaultMCPHTTPScope{Database: database, TokenID: 7, MetadataRead: func(context.Context, int64) (bool, error) { return true, nil }}, true
	})
	mcpScope, ok := mcp(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !ok || mcpScope.Database != database || mcpScope.TokenID != 7 || mcpScope.MetadataRead == nil {
		t.Fatalf("MCP scope was not preserved: scope=%#v ok=%v", mcpScope, ok)
	}
}
