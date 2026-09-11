package gatewayaccess

import (
	"errors"
	"net/http"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/componentstate"
)

type commandTestWorkspace struct{ state componentstate.State }

func (workspace *commandTestWorkspace) ComponentStatePort() componentstate.Port {
	return &workspace.state
}

func TestComponentsOwnIndependentAuthenticationState(t *testing.T) {
	first := NewComponent("3210")
	second := NewComponent("3212")
	first.RecordDatabasePasswordFailure("database")
	if first.DatabasePasswordFailureCount("database") != 1 {
		t.Fatal("first component did not retain its password failure")
	}
	if second.DatabasePasswordFailureCount("database") != 0 {
		t.Fatal("authentication state leaked between components")
	}
}

func TestCommandRuntimeFailsClosedUntilInitialized(t *testing.T) {
	workspace := &commandTestWorkspace{state: componentstate.New()}
	component := NewComponent("3210")
	if _, err := component.CommandRuntime(workspace); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("uninitialized command runtime error = %v", err)
	}
	if err := component.InitializeCommandRuntime(workspace, CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("invalid command runtime dependencies error = %v", err)
	}
	var unavailable *Component
	if err := unavailable.InitializeCommandRuntime(workspace, CommandRuntimeDependencies{}); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("nil component initialization error = %v", err)
	}
}

func TestNilComponentFailsClosed(t *testing.T) {
	var component *Component
	if err := component.WaitDatabasePassword(t.Context(), "database"); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("password wait error = %v", err)
	}
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost", nil)
	if _, err := component.AuthenticateMCP(request, func() []MCPTokenSource { return nil }); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("MCP authentication error = %v", err)
	}
	if component.AllowVaultReveal("vault") || component.AllowVaultGenerate("vault") || component.AllowVaultRequest("vault") {
		t.Fatal("nil access component allowed a Vault operation")
	}
}

func TestComponentOwnsCompleteHTTPHandlerSet(t *testing.T) {
	component := NewComponent("3212")
	handlers := component.HTTPHandlers(HTTPScopeProviders{})
	if handlers.Security == nil || handlers.TokenAccess == nil || handlers.BulkConsole == nil ||
		handlers.CommandRequests == nil || handlers.MCPRuntime == nil ||
		handlers.MCPConnectorReads == nil || handlers.MCPConnectorActions == nil {
		t.Fatal("access HTTP handler set is incomplete")
	}

	var unavailable *Component
	zero := unavailable.HTTPHandlers(HTTPScopeProviders{})
	if zero.Security != nil || zero.TokenAccess != nil || zero.BulkConsole != nil ||
		zero.CommandRequests != nil || zero.MCPRuntime != nil ||
		zero.MCPConnectorReads != nil || zero.MCPConnectorActions != nil {
		t.Fatal("nil access component exposed HTTP handlers")
	}
}
