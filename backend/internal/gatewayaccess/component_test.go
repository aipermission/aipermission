package gatewayaccess

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestComponentsOwnIndependentAuthenticationState(t *testing.T) {
	first := NewComponent("3210")
	second := NewComponent("3212")
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/unlock", nil)
	attempt, err := first.BeginPasswordAttempt(request, "database")
	if err != nil {
		t.Fatalf("begin password attempt: %v", err)
	}
	attempt.Failure()
	if first.databasePasswordFailureCount(attempt.key) != 1 {
		t.Fatal("first component did not retain its password failure")
	}
	if second.databasePasswordFailureCount(attempt.key) != 0 {
		t.Fatal("authentication state leaked between components")
	}
}

func TestPasswordAttemptIdentityIsSharedAcrossRoutesAndOmitsRequestMetadata(t *testing.T) {
	firstRequest := httptest.NewRequest(http.MethodPost, "/api/unlock", strings.NewReader(`{"password":"first-secret"}`))
	firstRequest.RemoteAddr = "127.0.0.1:12345"
	secondRequest := httptest.NewRequest(http.MethodPost, "/api/databases/delete-locked?database=customer-name", strings.NewReader(`{"current_password":"second-secret"}`))
	secondRequest.RemoteAddr = "127.0.0.1:54321"

	component := NewComponent("")
	first, err := component.BeginPasswordAttempt(firstRequest, "database-password")
	if err != nil {
		t.Fatalf("begin first password attempt: %v", err)
	}
	second, err := component.BeginPasswordAttempt(secondRequest, "database-password")
	if err != nil {
		t.Fatalf("begin second password attempt: %v", err)
	}
	if first.key != second.key {
		t.Fatalf("database password routes use different keys: %q != %q", first.key, second.key)
	}
	for _, sensitive := range []string{"unlock", "delete-locked", "customer-name", "first-secret", "second-secret"} {
		if strings.Contains(first.key, sensitive) {
			t.Fatalf("rate-limit key exposes request metadata %q: %q", sensitive, first.key)
		}
	}
	first.Failure()
	if component.databasePasswordFailureCount(first.key) != 1 {
		t.Fatal("failed password attempt was not recorded")
	}
	second.Success()
	if component.databasePasswordFailureCount(first.key) != 0 {
		t.Fatal("successful password attempt did not clear shared backoff")
	}
}

func TestCommandRuntimeFailsClosedUntilInitialized(t *testing.T) {
	component := NewComponent("3210")
	if _, err := component.CommandRuntime("runtime"); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("uninitialized command runtime error = %v", err)
	}
	if err := component.InitializeCommandRuntime("runtime", CommandRuntimeDependencies{}); !errors.Is(err, ErrCommandRuntimeUnavailable) {
		t.Fatalf("invalid command runtime dependencies error = %v", err)
	}
	var unavailable *Component
	if err := unavailable.InitializeCommandRuntime("runtime", CommandRuntimeDependencies{}); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("nil component initialization error = %v", err)
	}
}

func TestNilComponentFailsClosed(t *testing.T) {
	var component *Component
	request := httptest.NewRequest(http.MethodPost, "http://localhost/api/unlock", nil)
	if _, err := component.BeginPasswordAttempt(request, "database"); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("password wait error = %v", err)
	}
	if _, err := component.AuthenticateMCP(request, func() []MCPTokenSource { return nil }); !errors.Is(err, ErrComponentUnavailable) {
		t.Fatalf("MCP authentication error = %v", err)
	}
	if component.AllowVaultReveal("vault") || component.AllowVaultGenerate("vault") || component.AllowVaultRequest("vault") {
		t.Fatal("nil access component allowed a Vault operation")
	}
}

func TestPrincipalConstructionRequiresReadyRuntime(t *testing.T) {
	var component *Component
	runtime := testRuntimeIdentity{workspaceID: "workspace", runtimeID: "runtime", ready: true}
	local, err := component.LocalPrincipal(runtime)
	if err != nil || !local.IsLocalOperator() {
		t.Fatalf("local principal = %#v, %v", local, err)
	}
	token, err := component.TokenPrincipal(runtime, 7)
	if err != nil || !token.IsMCPToken() || token.TokenID != 7 {
		t.Fatalf("token principal = %#v, %v", token, err)
	}
	if _, err := component.LocalPrincipal(testRuntimeIdentity{}); !errors.Is(err, ErrInvalidPrincipal) {
		t.Fatalf("unready runtime error = %v", err)
	}
}

type testRuntimeIdentity struct {
	workspaceID string
	runtimeID   string
	ready       bool
}

func (runtime testRuntimeIdentity) WorkspaceIdentifier() string { return runtime.workspaceID }
func (runtime testRuntimeIdentity) RuntimeIdentifier() string   { return runtime.runtimeID }
func (runtime testRuntimeIdentity) IdentityReady() bool         { return runtime.ready }

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
