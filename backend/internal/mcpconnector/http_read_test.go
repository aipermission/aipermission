package mcpconnector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestReadHandlersFailClosedWithoutCompositionScope(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter, *http.Request) (Scope, bool) {
		return Scope{}, true
	})
	for name, handler := range map[string]http.HandlerFunc{
		"targets": handlers.ListTargets,
		"help":    handlers.GetHelp,
		"actions": handlers.GetActions,
	} {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler(response, httptest.NewRequest(http.MethodGet, "/", nil))
			if response.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
		})
	}
}
