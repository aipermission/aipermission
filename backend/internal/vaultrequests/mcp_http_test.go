package vaultrequests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestMCPHandlersFailClosedWithoutCompositionScope(t *testing.T) {
	handlers := NewMCPHTTPHandlers(func(http.ResponseWriter, *http.Request) (MCPHTTPScope, bool) {
		return MCPHTTPScope{}, true
	})
	response := httptest.NewRecorder()
	handlers.ListItems(response, httptest.NewRequest(http.MethodGet, "/api/mcp/vault-items", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestMCPCallPreservesStoppedPrecedenceBeforeBodyDecode(t *testing.T) {
	harness := newRuntimeHarness(t)
	secretVault, err := vault.New("mcp-http-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewMCPHTTPHandlers(func(http.ResponseWriter, *http.Request) (MCPHTTPScope, bool) {
		return MCPHTTPScope{
			Database: harness.database, Vault: secretVault, WorkspaceUUID: "mcp-http-workspace",
			TokenID: 1, MCPStarted: func() bool { return false },
			Runtime:      func(context.Context) (*Runtime, error) { return harness.runtime, nil },
			MetadataRead: func(context.Context, int64) (bool, error) { return true, nil },
		}, true
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/mcp/vault-actions/call", strings.NewReader("not-json"))
	handlers.Call(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"stopped"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}
