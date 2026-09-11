package gatewayoperations

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackupApplicationMissingOperationLeaseFailsClosed(t *testing.T) {
	application := NewBackupApplication(BackupDependencies{})
	handlers := application.HTTPHandlers()
	response := httptest.NewRecorder()
	handlers.Download(response, httptest.NewRequest(http.MethodGet, "/api/database/download", nil))
	if response.Code != http.StatusRequestTimeout {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestTimeout)
	}
}
