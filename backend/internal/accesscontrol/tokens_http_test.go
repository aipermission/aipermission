package accesscontrol

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func TestCreateTokenRequiresOnlyItsDeclaredCapabilities(t *testing.T) {
	database := openTestDatabase(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens:         tokens.NewStore(database),
			ReusableTokens: func(context.Context) (bool, error) { return false, nil },
			Mutate:         auditRunner(database, nil),
		}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tokens", handlers.CreateToken)
	request := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name":"agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("create token: %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("sensitive response headers = %#v", response.Header())
	}
	if countRows(t, database, "api_tokens") != 1 || countRows(t, database, "audit_logs") != 1 {
		t.Fatal("token and audit were not committed together")
	}
}

func TestCreateTokenFailsClosedWithoutMutationRunner(t *testing.T) {
	database := openTestDatabase(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (Scope, bool) {
		return Scope{
			Tokens:         tokens.NewStore(database),
			ReusableTokens: func(context.Context) (bool, error) { return false, nil },
		}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/tokens", bytes.NewBufferString(`{"name":"agent"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlers.CreateToken(response, request)

	if response.Code != http.StatusInternalServerError || countRows(t, database, "api_tokens") != 0 {
		t.Fatalf("missing mutation runner: %d %s", response.Code, response.Body.String())
	}
}
