package projectvault

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/httptransport"
)

func TestHTTPHandlersCreateAndRevealSensitiveItem(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
	})

	create := vaultHTTPRequest(t, http.MethodPost, "/api/vault-items", `{
		"name":"HTTP_SECRET","value":"http-secret-value","owner_project_id":`+
		strconv.FormatInt(harness.projectID, 10)+`,"secret_type":"generic_secret","source":"imported"
	}`)
	created := httptest.NewRecorder()
	handlers.CreateItem(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.Code, created.Body.String())
	}
	var item Item
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	if item.Name != "HTTP_SECRET" || strings.Contains(created.Body.String(), "http-secret-value") {
		t.Fatalf("created item = %s", created.Body.String())
	}

	reveal := httptest.NewRequest(http.MethodPost, "/api/vault-items/1/reveal", strings.NewReader(`{}`))
	reveal.SetPathValue("id", strconv.FormatInt(item.ID, 10))
	revealed := httptest.NewRecorder()
	handlers.RevealItem(revealed, reveal)
	if revealed.Code != http.StatusOK || revealed.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("reveal response = %d headers %#v body %s", revealed.Code, revealed.Header(), revealed.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(revealed.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["value"] != "http-secret-value" {
		t.Fatalf("revealed payload = %#v", payload)
	}
}

func TestHTTPHandlersPreserveFailClosedTransportContracts(t *testing.T) {
	t.Run("locked scope", func(t *testing.T) {
		handlers := NewHTTPHandlers(func(w http.ResponseWriter) (HTTPScope, bool) {
			httptransport.WriteError(w, http.StatusLocked, "database is locked")
			return HTTPScope{}, false
		})
		response := httptest.NewRecorder()
		handlers.ListItems(response, httptest.NewRequest(http.MethodGet, "/api/vault-items", nil))
		if response.Code != http.StatusLocked {
			t.Fatalf("status = %d", response.Code)
		}
	})

	t.Run("strict json", func(t *testing.T) {
		harness := newRuntimeTestHarness(t)
		handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
			return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
		})
		response := httptest.NewRecorder()
		handlers.CreateItem(response, vaultHTTPRequest(t, http.MethodPost, "/api/vault-items", `{"unexpected":true}`))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
		}
	})

	t.Run("canceled mutation", func(t *testing.T) {
		harness := newRuntimeTestHarness(t)
		harness.delivery.err = context.Canceled
		handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
			return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
		})
		response := httptest.NewRecorder()
		handlers.CreateItem(response, vaultHTTPRequest(t, http.MethodPost, "/api/vault-items", `{}`))
		if response.Code != http.StatusRequestTimeout {
			t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
		}
	})
}

func TestHTTPHandlersRejectMalformedFiltersAndIDs(t *testing.T) {
	harness := newRuntimeTestHarness(t)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Runtime: harness.runtime, RuntimeID: "workspace-one"}, true
	})
	for _, path := range []string{
		"/api/vault-items?project_id=zero",
		"/api/vault-items?limit=many",
		"/api/vault-items?offset=next",
		"/api/vault-default-bindings?profile_id=-1",
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if strings.Contains(path, "default-bindings") {
			handlers.ListDefaultBindings(response, request)
		} else {
			handlers.ListItems(response, request)
		}
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d body = %s", path, response.Code, response.Body.String())
		}
	}

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/vault-items/nope", nil)
	request.SetPathValue("id", "nope")
	handlers.GetItem(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid id status = %d", response.Code)
	}
}

func vaultHTTPRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}
