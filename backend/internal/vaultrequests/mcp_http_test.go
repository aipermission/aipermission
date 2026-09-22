package vaultrequests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	projectstore "github.com/aipermission/aipermission/backend/internal/projects"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type deliveryCheckingResponseWriter struct {
	*httptest.ResponseRecorder
	t       *testing.T
	held    func() bool
	checked bool
}

func (writer *deliveryCheckingResponseWriter) WriteHeader(status int) {
	writer.t.Helper()
	if !writer.held() {
		writer.t.Error("Vault response headers were written without delivery admission")
	}
	writer.checked = true
	writer.ResponseRecorder.WriteHeader(status)
}

func (writer *deliveryCheckingResponseWriter) Write(payload []byte) (int, error) {
	writer.t.Helper()
	if !writer.held() {
		writer.t.Error("Vault response body was written without delivery admission")
	}
	writer.checked = true
	return writer.ResponseRecorder.Write(payload)
}

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
			Runtime:      func(context.Context) (Application, error) { return harness.runtime, nil },
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

func TestMCPCallHoldsDeliveryAdmissionThroughResponseWrite(t *testing.T) {
	harness := newRuntimeHarness(t)
	secretVault, err := vault.New("mcp-http-delivery-secret")
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewMCPHTTPHandlers(func(http.ResponseWriter, *http.Request) (MCPHTTPScope, bool) {
		return MCPHTTPScope{
			Database: harness.database, Vault: secretVault, WorkspaceUUID: "mcp-http-workspace",
			TokenID: harness.tokenID, MCPStarted: func() bool { return true },
			Runtime:      func(context.Context) (Application, error) { return harness.runtime, nil },
			MetadataRead: func(context.Context, int64) (bool, error) { return true, nil },
		}, true
	})
	recorder := httptest.NewRecorder()
	response := &deliveryCheckingResponseWriter{ResponseRecorder: recorder, t: t, held: harness.delivery.held}
	body := fmt.Sprintf(`{"project_ref":%q,"action_name":%q,"input":{"name":"PROJECT_KEY","generator_kind":"hex_32"},"reason":"create a deployment key","idempotency_key":"http-delivery"}`,
		harness.projectRef, ActionGenerateItem)
	request := httptest.NewRequest(http.MethodPost, "/api/mcp/vault-actions/call", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handlers.Call(response, request)
	if recorder.Code != http.StatusOK || !response.checked || harness.delivery.held() {
		t.Fatalf("response=%d checked=%v held=%v body=%s", recorder.Code, response.checked, harness.delivery.held(), recorder.Body.String())
	}
}

func TestMCPListRejectsWhitespaceProjectReference(t *testing.T) {
	harness := newRuntimeHarness(t)
	secretVault, err := vault.New("mcp-http-list-secret")
	if err != nil {
		t.Fatal(err)
	}
	handlers := NewMCPHTTPHandlers(func(http.ResponseWriter, *http.Request) (MCPHTTPScope, bool) {
		return MCPHTTPScope{
			Database: harness.database, Vault: secretVault, WorkspaceUUID: "mcp-http-workspace",
			TokenID: harness.tokenID, Runtime: func(context.Context) (Application, error) { return harness.runtime, nil },
			MetadataRead: func(context.Context, int64) (bool, error) { return true, nil },
		}, true
	})
	response := httptest.NewRecorder()
	handlers.ListItems(response, httptest.NewRequest(http.MethodGet, "/api/mcp/vault-items?project_ref=%20%20", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
}

func TestMCPProjectReferenceErrorsAreClientErrors(t *testing.T) {
	for _, err := range []error{
		projectstore.ValidationError("project id reference must be a positive integer"),
		fmt.Errorf("%w: use id:42 or slug:42", projectstore.ErrAmbiguousRef),
	} {
		response := httptest.NewRecorder()
		if !writeMCPCallError(response, err) {
			t.Fatalf("error %v was not handled", err)
		}
		if response.Code != http.StatusBadRequest {
			t.Fatalf("error %v status = %d, want %d", err, response.Code, http.StatusBadRequest)
		}
	}
}

func TestMCPExpiredIdempotencyResultIsGone(t *testing.T) {
	response := httptest.NewRecorder()
	if !writeMCPCallError(response, ErrIdempotencyExpired) {
		t.Fatal("expired idempotency error was not handled")
	}
	if response.Code != http.StatusGone {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusGone)
	}
}
