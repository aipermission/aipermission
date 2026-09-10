package mcpconnector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/actions"
)

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
