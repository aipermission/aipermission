package mcpconnector

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/executionprincipal"
	"github.com/aipermission/aipermission/backend/internal/runtimecontrol"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vaultsessions"
)

type acceptingDeliveryGate struct{}

func (acceptingDeliveryGate) Acquire(context.Context) (func(), error) { return func() {}, nil }

func TestActionHandlersFailClosedWithoutCompositionScope(t *testing.T) {
	handlers := NewActionHTTPHandlers(func(http.ResponseWriter, *http.Request) (ActionScope, bool) {
		return ActionScope{}, true
	})
	for name, handler := range map[string]http.HandlerFunc{"call": handlers.Call, "get": handlers.GetRequest} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
		})
	}
}

func TestWriteResourceLimitReturnsBoundedRetryAfter(t *testing.T) {
	for _, retry := range []time.Duration{0, 5 * time.Second, 5 * time.Minute} {
		response := httptest.NewRecorder()
		writeResourceLimit(response, retry)
		seconds, err := strconv.Atoi(response.Header().Get("Retry-After"))
		if err != nil || seconds < 1 || seconds > 60 {
			t.Fatalf("retry %v header = %q", retry, response.Header().Get("Retry-After"))
		}
		if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "connector_action_backpressure") {
			t.Fatalf("response = %d %s", response.Code, response.Body.String())
		}
	}
}

func TestActionHandlerRateLimitRejectsBeforeCall(t *testing.T) {
	database := &sql.DB{}
	called := false
	scope := ActionScope{
		Database:  database,
		RuntimeID: "runtime-one",
		TokenID:   9,
		Output: &OutputAuthorization{
			Database: database, Tokens: tokens.NewStore(database), Leases: vaultsessions.NewStore(),
			Delivery: acceptingDeliveryGate{}, MCPStarted: func() bool { return true },
			Principal: func(int64) (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		},
		ActionVisible: func(context.Context, string, string) (bool, error) { return true, nil },
		ReplayExists:  func(context.Context, string) (bool, error) { return false, nil },
		ResourcePolicy: func(context.Context, string, string) (ActionResourcePolicy, error) {
			return ActionResourcePolicy{MaxInputBytes: 1024}, nil
		},
		Call: func(context.Context, ActionCall) (ActionCallResult, error) {
			called = true
			return ActionCallResult{}, nil
		},
		Observe: func(context.Context, string, any) {},
		Redact:  func(_ context.Context, value string) string { return value },
	}
	handlers := NewActionHTTPHandlers(func(http.ResponseWriter, *http.Request) (ActionScope, bool) { return scope, true })
	handlers.admission = runtimecontrol.NewAdmission(1, time.Minute, 1)

	first := httptest.NewRecorder()
	handlers.Call(first, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{")))
	if first.Code != http.StatusBadRequest {
		t.Fatalf("first status = %d", first.Code)
	}
	second := httptest.NewRecorder()
	handlers.Call(second, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}")))
	if second.Code != http.StatusTooManyRequests || second.Header().Get("Retry-After") == "" {
		t.Fatalf("second response = %d %#v", second.Code, second.Header())
	}
	if called {
		t.Fatal("rate-limited request reached connector call")
	}
}

func TestActionHandlerAdmissionIsScopedByWorkspace(t *testing.T) {
	database := &sql.DB{}
	runtimeID := "runtime-one"
	scope := ActionScope{
		Database: database, RuntimeID: runtimeID, TokenID: 9,
		Output: &OutputAuthorization{
			Database: database, Tokens: tokens.NewStore(database), Leases: vaultsessions.NewStore(),
			Delivery: acceptingDeliveryGate{}, MCPStarted: func() bool { return true },
			Principal: func(int64) (executionprincipal.Principal, error) { return executionprincipal.Principal{}, nil },
		},
		ActionVisible: func(context.Context, string, string) (bool, error) { return true, nil },
		ReplayExists:  func(context.Context, string) (bool, error) { return false, nil },
		ResourcePolicy: func(context.Context, string, string) (ActionResourcePolicy, error) {
			return ActionResourcePolicy{MaxInputBytes: 1024}, nil
		},
		Call:    func(context.Context, ActionCall) (ActionCallResult, error) { return ActionCallResult{}, nil },
		Observe: func(context.Context, string, any) {},
		Redact:  func(_ context.Context, value string) string { return value },
	}
	handlers := NewActionHTTPHandlers(func(http.ResponseWriter, *http.Request) (ActionScope, bool) {
		scope.RuntimeID = runtimeID
		return scope, true
	})
	handlers.admission = runtimecontrol.NewAdmission(1, time.Minute, 1)

	first := httptest.NewRecorder()
	handlers.Call(first, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{")))
	if first.Code != http.StatusBadRequest {
		t.Fatalf("first workspace status = %d", first.Code)
	}
	runtimeID = "runtime-two"
	second := httptest.NewRecorder()
	handlers.Call(second, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{")))
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second workspace status = %d", second.Code)
	}
}

func TestActionErrorPreservesTerminalPersistenceAndStoppedContracts(t *testing.T) {
	scope := ActionScope{Redact: func(_ context.Context, value string) string { return value }}
	tests := []struct {
		name   string
		err    error
		status int
		text   string
	}{
		{"persistence", actions.NewTerminalPersistenceError(42, errors.New("store failed")), http.StatusServiceUnavailable, `"request_id":42`},
		{"stopped", actions.ErrMCPExecutionStopped, http.StatusOK, `"status":"stopped"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			writeActionError(response, httptest.NewRequest(http.MethodPost, "/", nil), scope, test.err)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.text) {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
}
