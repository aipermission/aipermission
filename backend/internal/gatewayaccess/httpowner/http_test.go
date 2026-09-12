package httpowner

import (
	"testing"

	"github.com/aipermission/aipermission/backend/internal/gatewayaccess"
)

func TestNewOwnsCompleteHTTPHandlerSet(t *testing.T) {
	handlers := New(gatewayaccess.NewComponent("3212"), ScopeProviders{})
	if handlers.Security == nil || handlers.TokenAccess == nil || handlers.MCPRuntime == nil ||
		handlers.MCPConnectorReads == nil || handlers.MCPConnectorActions == nil {
		t.Fatal("access HTTP handler set is incomplete")
	}

	zero := New(nil, ScopeProviders{})
	if zero.Security != nil || zero.TokenAccess != nil || zero.MCPRuntime != nil ||
		zero.MCPConnectorReads != nil || zero.MCPConnectorActions != nil {
		t.Fatal("nil access component exposed HTTP handlers")
	}
}
