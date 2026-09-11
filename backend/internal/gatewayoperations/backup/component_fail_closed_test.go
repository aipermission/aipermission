package backup

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComponentMissingOperationLeaseFailsClosed(t *testing.T) {
	application := New(Dependencies{})
	handlers := application.HTTPHandlers()
	response := httptest.NewRecorder()
	handlers.Download(response, httptest.NewRequest(http.MethodGet, "/api/database/download", nil))
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestTimeout)
	}
}
