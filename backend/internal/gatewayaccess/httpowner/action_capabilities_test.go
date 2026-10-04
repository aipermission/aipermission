package httpowner

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

func TestActionAdapterKeepsMissingResourcePolicyUnavailable(t *testing.T) {
	// A missing capability must be rejected before storage or execution is used.
	database := &sql.DB{}
	scope := gatewayaccess.MCPActionScope{
		Database: database, RuntimeID: "fixture-runtime", TokenID: 1,
		Output: &gatewayaccess.MCPOutputAuthorization{
			Database: database, Tokens: &tokens.Store{}, Leases: &vaultsessions.Store{},
			Delivery: unavailableDeliveryGate{}, MCPStarted: func() bool { return true },
			Principal: func(int64) (gatewayaccess.Principal, error) {
				t.Fatal("missing policy reached output authorization")
				return gatewayaccess.Principal{}, nil
			},
		},
		ActionVisible: func(context.Context, string, string) (bool, error) {
			t.Fatal("missing policy reached visibility evaluation")
			return true, nil
		},
		ReplayExists: func(context.Context, string) (bool, error) {
			t.Fatal("missing policy reached replay lookup")
			return false, nil
		},
		Call: func(context.Context, gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error) {
			t.Fatal("missing policy reached execution")
			return gatewayaccess.MCPActionCallResult{}, nil
		},
		Observe: func(context.Context, string, any) { t.Fatal("missing policy emitted an observation") },
		Redact:  func(_ context.Context, value string) string { return value },
	}
	handlers := (Factory{}).Build(gatewayaccess.ScopeProviders{
		MCPConnectorActions: func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPActionScope, bool) {
			return scope, true
		},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{
		"target_ref":"fixture:1:1","action_name":"read","input":{},"reason":"fixture","idempotency_key":"fixture"
	}`))
	request.Header.Set("Content-Type", "application/json")
	handlers.MCPConnectorActions.Call(response, request)
	if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "fixture-runtime") {
		t.Fatalf("missing policy was not rejected generically: %d %s", response.Code, response.Body.String())
	}
	adapted, ok := adaptMCPActionScopeProvider(func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPActionScope, bool) {
		return scope, true
	})(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !ok || adapted.ResourcePolicy != nil {
		t.Fatal("adapter manufactured an unavailable policy capability")
	}
}

type unavailableDeliveryGate struct{}

func (unavailableDeliveryGate) Acquire(context.Context) (func(), error) {
	return nil, context.Canceled
}

func TestActionAdapterWithPolicyReachesConfiguredWorkflow(t *testing.T) {
	database := &sql.DB{}
	steps := []string{}
	scope := gatewayaccess.MCPActionScope{
		Database: database, RuntimeID: "fixture-runtime", TokenID: 1,
		Output: &gatewayaccess.MCPOutputAuthorization{
			Database: database, Tokens: &tokens.Store{}, Leases: &vaultsessions.Store{},
			Delivery: unavailableDeliveryGate{}, MCPStarted: func() bool { return true },
			Principal: func(int64) (gatewayaccess.Principal, error) { return gatewayaccess.Principal{}, nil },
		},
		ActionVisible: func(context.Context, string, string) (bool, error) {
			steps = append(steps, "visibility")
			return true, nil
		},
		ReplayExists: func(context.Context, string) (bool, error) {
			steps = append(steps, "replay")
			return false, nil
		},
		ResourcePolicy: func(context.Context, string, string) (gatewayaccess.MCPActionResourcePolicy, error) {
			steps = append(steps, "policy")
			return gatewayaccess.MCPActionResourcePolicy{MaxInputBytes: 1024}, nil
		},
		Call: func(_ context.Context, call gatewayaccess.MCPActionCall) (gatewayaccess.MCPActionCallResult, error) {
			steps = append(steps, "call")
			if call.Source != "mcp" || call.TokenID != 1 || call.TargetRef != "fixture:1:1" || call.ActionName != "read" ||
				call.Reason != "fixture" || call.IdempotencyKey != "fixture" {
				t.Fatalf("HTTP call identity changed: %#v", call)
			}
			return gatewayaccess.MCPActionCallResult{}, actions.ErrMCPExecutionStopped
		},
		Observe: func(context.Context, string, any) { t.Fatal("stopped workflow emitted a completion observation") },
		Redact:  func(_ context.Context, value string) string { return value },
	}
	handlers := (Factory{}).Build(gatewayaccess.ScopeProviders{
		MCPConnectorActions: func(http.ResponseWriter, *http.Request) (gatewayaccess.MCPActionScope, bool) {
			return scope, true
		},
	})
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{
		"target_ref":"fixture:1:1","action_name":"read","input":{},"reason":"fixture","idempotency_key":"fixture"
	}`))
	r.Header.Set("Content-Type", "application/json")
	handlers.MCPConnectorActions.Call(w, r)
	if strings.Join(steps, ",") != "replay,visibility,policy,call" || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"stopped"`) {
		t.Fatalf("configured workflow not reached: steps=%v HTTP=%d %s", steps, w.Code, w.Body.String())
	}
}
