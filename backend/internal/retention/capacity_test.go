package retention

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectortargets/actioncapacity"
)

func TestCapacityHandlerReturnsPrivateCurrentLimitsAndRetention(t *testing.T) {
	t.Setenv("AIPERMISSION_CONNECTOR_REQUEST_STORAGE_MIB", "1024")
	database := openTestDatabase(t)
	if _, err := database.Exec(`INSERT INTO settings(key,value,updated_at)
	 VALUES ('retention_history_days','2',datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	handler := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: NewService(database, "workspace", nil)}, true
	})
	response := httptest.NewRecorder()
	handler.Capacity(response, httptest.NewRequest(http.MethodGet, "/api/settings/connector-capacity", nil))
	var report struct {
		ByteLimit   int64 `json:"byte_limit"`
		HistoryDays int   `json:"history_days"`
		Items       []any `json:"items"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || report.ByteLimit != 1024<<20 || report.HistoryDays != 2 || report.Items == nil || response.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatalf("capacity response=%d %s headers=%v", response.Code, response.Body.String(), response.Header())
	}
	for _, statement := range []string{"DROP TABLE settings", "DROP TABLE connector_action_request_usage"} {
		if _, err := database.Exec(statement); err != nil {
			t.Fatal(err)
		}
		response = httptest.NewRecorder()
		handler.Capacity(response, httptest.NewRequest(http.MethodGet, "/api/settings/connector-capacity", nil))
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "no such table") {
			t.Fatalf("failed read response=%d %s", response.Code, response.Body.String())
		}
	}
}

func TestCapacityHandlerRejectsMissingServicesAndInvalidRuntimeConfiguration(t *testing.T) {
	for _, handler := range []*HTTPHandlers{nil, NewHTTPHandlers(nil), NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) { return HTTPScope{}, true }), NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: NewService(nil, "workspace", nil)}, true
	})} {
		response := httptest.NewRecorder()
		handler.Capacity(response, httptest.NewRequest(http.MethodGet, "/api/settings/connector-capacity", nil))
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("nil dependency status=%d", response.Code)
		}
	}
	t.Setenv("AIPERMISSION_CONNECTOR_REQUEST_STORAGE_MIB", "invalid")
	database := openTestDatabase(t)
	handler := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{Service: NewService(database, "workspace", nil)}, true
	})
	response := httptest.NewRecorder()
	handler.Capacity(response, httptest.NewRequest(http.MethodGet, "/api/settings/connector-capacity", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("invalid runtime status=%d", response.Code)
	}
	if actioncapacity.MaxBytes != 256<<20 {
		t.Fatal("default changed")
	}
}
