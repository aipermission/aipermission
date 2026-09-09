package observability

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gatewaydb "github.com/aipermission/aipermission/backend/internal/db"
)

func TestAuditHTTPHandlersListFilterPaginationAndDetail(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-http.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	firstID := insertQueryFixture(t, database, `
		INSERT INTO audit_logs (actor_type, action, lifecycle_phase, payload_json, created_at)
		VALUES ('user', 'first.action', 'observed', '{"value":"first"}', '2026-01-01T00:00:00Z')`)
	insertQueryFixture(t, database, `
		INSERT INTO audit_logs (actor_type, action, lifecycle_phase, payload_json, created_at)
		VALUES ('mcp', 'second.action', 'completed', '{"value":"second"}', '2026-01-02T00:00:00Z')`)
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Database: database}, true
	})

	list := httptest.NewRecorder()
	handlers.List(list, httptest.NewRequest(http.MethodGet, "/api/audit-logs?actor=user&limit=1", nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"total":1`) || !strings.Contains(list.Body.String(), "first.action") {
		t.Fatalf("list response: %d %s", list.Code, list.Body.String())
	}

	detailRequest := httptest.NewRequest(http.MethodGet, "/api/audit-logs/1", nil)
	detailRequest.SetPathValue("id", strconv.FormatInt(firstID, 10))
	detail := httptest.NewRecorder()
	handlers.Get(detail, detailRequest)
	if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), "first.action") || !strings.Contains(detail.Body.String(), `\"value\":\"first\"`) {
		t.Fatalf("detail response: %d %s", detail.Code, detail.Body.String())
	}

	missingRequest := httptest.NewRequest(http.MethodGet, "/api/audit-logs/999", nil)
	missingRequest.SetPathValue("id", "999")
	missing := httptest.NewRecorder()
	handlers.Get(missing, missingRequest)
	if missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "audit log not found") {
		t.Fatalf("missing response: %d %s", missing.Code, missing.Body.String())
	}
}

func TestAuditHTTPHandlersRejectInvalidPaginationAndIdentity(t *testing.T) {
	database, err := gatewaydb.OpenEncrypted(filepath.Join(t.TempDir(), "audit-invalid-http.db"), "test-password")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return HTTPScope{Database: database}, true })
	for _, target := range []string{
		"/api/audit-logs?limit=0",
		"/api/audit-logs?offset=-1",
		"/api/audit-logs?runtime_id=nope",
	} {
		response := httptest.NewRecorder()
		handlers.List(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("target=%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}
