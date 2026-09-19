package securitypolicy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPHandlersPreserveSecurityPolicyContract(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: service, Mutate: auditRunner(database, nil)}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /settings", handlers.GetSettings)
	mux.HandleFunc("PUT /settings", handlers.UpdateSettings)
	mux.HandleFunc("GET /rules", handlers.ListRules)
	mux.HandleFunc("POST /rules", handlers.CreateRule)
	mux.HandleFunc("PUT /rules/{id}", handlers.UpdateRule)
	mux.HandleFunc("DELETE /rules/{id}", handlers.DeleteRule)

	response := performRequest(mux, http.MethodPut, "/settings", `{"reusable_tokens":true,"unknown":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown settings field response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodPut, "/settings", `{"reusable_tokens":true,"expose_mcp_server_metadata":false,"mcp_start_enabled":false,"redaction_mode":"off"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "revision is required") {
		t.Fatalf("missing revision response = %d %s", response.Code, response.Body.String())
	}
	loaded := performRequest(mux, http.MethodGet, "/settings", "")
	var document SettingsDocument
	if loaded.Code != http.StatusOK || json.Unmarshal(loaded.Body.Bytes(), &document) != nil || document.Revision == "" {
		t.Fatalf("settings document response = %d %s", loaded.Code, loaded.Body.String())
	}
	response = performRequest(mux, http.MethodPut, "/settings", `{"reusable_tokens":true,"redaction_mode":"off","expected_revision":"`+document.Revision+`"}`)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "all security settings are required") {
		t.Fatalf("incomplete settings response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodPut, "/settings", `{"reusable_tokens":true,"expose_mcp_server_metadata":false,"mcp_start_enabled":false,"redaction_mode":"off","expected_revision":"`+document.Revision+`"}`)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"redaction_mode":"off"`) {
		t.Fatalf("settings update response = %d %s", response.Code, response.Body.String())
	}

	response = performRequest(mux, http.MethodPost, "/rules", `{"name":"broken","pattern":"[","enabled":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodPost, "/rules", `{"name":"token","pattern":"token_[a-z]+","enabled":true}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("create rule response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodPost, "/rules", `{"name":"token","pattern":"other","enabled":true}`)
	if response.Code != http.StatusConflict {
		t.Fatalf("duplicate rule response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodPut, "/rules/not-an-id", `{"name":"token","pattern":"other","enabled":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule id response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodDelete, "/rules/999", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing rule response = %d %s", response.Code, response.Body.String())
	}
	response = performRequest(mux, http.MethodGet, "/rules", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"name":"token"`) {
		t.Fatalf("list rules response = %d %s", response.Code, response.Body.String())
	}
}

func TestHTTPHandlersRejectStaleSecuritySettingsRevision(t *testing.T) {
	database := openTestDatabase(t)
	service := NewService(database)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: service, Mutate: auditRunner(database, nil)}, true
	})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /settings", handlers.GetSettings)
	mux.HandleFunc("PUT /settings", handlers.UpdateSettings)

	loaded := performRequest(mux, http.MethodGet, "/settings", "")
	var baseline SettingsDocument
	if err := json.Unmarshal(loaded.Body.Bytes(), &baseline); err != nil {
		t.Fatal(err)
	}
	first := performRequest(mux, http.MethodPut, "/settings", `{"reusable_tokens":true,"expose_mcp_server_metadata":false,"mcp_start_enabled":false,"redaction_mode":"basic","expected_revision":"`+baseline.Revision+`"}`)
	if first.Code != http.StatusOK {
		t.Fatalf("first settings update = %d %s", first.Code, first.Body.String())
	}
	stale := performRequest(mux, http.MethodPut, "/settings", `{"reusable_tokens":false,"expose_mcp_server_metadata":true,"mcp_start_enabled":false,"redaction_mode":"basic","expected_revision":"`+baseline.Revision+`"}`)
	if stale.Code != http.StatusConflict || !strings.Contains(stale.Body.String(), "changed in another client") {
		t.Fatalf("stale settings update = %d %s", stale.Code, stale.Body.String())
	}
	current, err := service.ReadSettings(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !current.ReusableTokens || current.ExposeMCPServerMetadata {
		t.Fatalf("stale write replaced committed settings: %#v", current)
	}
}

func TestHTTPHandlersRejectMissingMutationCapability(t *testing.T) {
	database := openTestDatabase(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: NewService(database)}, true
	})
	response := httptest.NewRecorder()
	handlers.UpdateSettings(response, jsonRequest(http.MethodPut, "/settings", `{"redaction_mode":"basic"}`))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func performRequest(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	handler.ServeHTTP(response, request)
	return response
}

func jsonRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
