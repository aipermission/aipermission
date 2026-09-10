package commandrequests

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHTTPHandlersGetCommandRequest(t *testing.T) {
	database, runtimeID := commandRequestFixture(t)
	result, err := database.ExecContext(t.Context(), `
		INSERT INTO command_requests (runtime_id, source, command, reason, status, created_at)
		VALUES (?, 'console', 'uptime', 'inspect load', 'completed', datetime('now'))`, runtimeID)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPReader, bool) { return NewStore(database), true })
	request := httptest.NewRequest(http.MethodGet, "/api/console/command-requests/1", nil)
	request.SetPathValue("id", strconv.FormatInt(id, 10))
	response := httptest.NewRecorder()
	handlers.Get(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"command":"uptime"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPHandlersValidateIDBeforeScopeAndConcealMissingRequests(t *testing.T) {
	scopeCalls := 0
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPReader, bool) {
		scopeCalls++
		return nil, false
	})
	invalidRequest := httptest.NewRequest(http.MethodGet, "/api/console/command-requests/nope", nil)
	invalidRequest.SetPathValue("id", "nope")
	invalid := httptest.NewRecorder()
	handlers.Get(invalid, invalidRequest)
	if invalid.Code != http.StatusBadRequest || scopeCalls != 0 {
		t.Fatalf("invalid response = %d scope calls = %d", invalid.Code, scopeCalls)
	}

	database, _ := commandRequestFixture(t)
	handlers = NewHTTPHandlers(func(http.ResponseWriter) (HTTPReader, bool) { return NewStore(database), true })
	missingRequest := httptest.NewRequest(http.MethodGet, "/api/console/command-requests/999", nil)
	missingRequest.SetPathValue("id", "999")
	missing := httptest.NewRecorder()
	handlers.Get(missing, missingRequest)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing response = %d %s", missing.Code, missing.Body.String())
	}
}
