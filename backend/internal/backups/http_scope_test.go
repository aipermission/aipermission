package backups

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderCatalogDoesNotRequireAnUnlockedWorkspace(t *testing.T) {
	handlers := NewHTTPHandlers(nil)
	response := httptest.NewRecorder()
	handlers.ProviderCatalog(response, httptest.NewRequest(http.MethodGet, "/api/backup/providers/catalog", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), ServiceProviderType) {
		t.Fatalf("catalog response = %d %s", response.Code, response.Body.String())
	}
}

func TestProviderHandlersFailClosedWhenRequiredScopePortsAreMissing(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{}, true
	})
	request := httptest.NewRequest(http.MethodGet, "/api/backup/providers", nil)
	response := httptest.NewRecorder()
	handlers.ListProviders(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestEnableProviderFailsClosedWithoutPasswordAuthorizationPort(t *testing.T) {
	handlers := NewHTTPHandlers(func(http.ResponseWriter) (HTTPScope, bool) {
		return HTTPScope{}, true
	})
	request := httptest.NewRequest(http.MethodPost, "/api/backup/providers/1/enable", strings.NewReader(`{"current_password":"secret"}`))
	request.SetPathValue("id", "1")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handlers.EnableProvider(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestProviderOperationsRejectIncompleteCompositionScope(t *testing.T) {
	if _, err := EnableProvider(context.Background(), HTTPScope{}, 1); !strings.Contains(err.Error(), "scope is incomplete") {
		t.Fatalf("enable error = %v", err)
	}
	if _, err := PrepareProviderRestore(context.Background(), HTTPScope{}, 1, 1); !strings.Contains(err.Error(), "scope is incomplete") {
		t.Fatalf("restore error = %v", err)
	}
}

func TestProviderHTTPErrorPreservesRemoteTimeoutSemantics(t *testing.T) {
	response := httptest.NewRecorder()
	WriteProviderHTTPError(response, remoteOperationError{err: context.DeadlineExceeded})
	if response.Code != http.StatusGatewayTimeout || !strings.Contains(response.Body.String(), "timed out") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestProviderHTTPErrorPreservesDisabledConflict(t *testing.T) {
	response := httptest.NewRecorder()
	WriteProviderHTTPError(response, ErrProviderDisabled)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "provider is disabled") {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestRemoteBackupFreshnessUsesKnownVersionAndTimestamp(t *testing.T) {
	remote := ServiceBackup{ID: "bkp_new", CreatedAt: "2026-07-31T12:00:00Z"}
	if !remoteBackupIsNewer(remote, nil) {
		t.Fatal("a remote version without a local baseline should be reported")
	}
	baseline := &ServiceBaseline{BackupID: "bkp_old", CreatedAt: "2026-07-31T11:00:00Z"}
	if !remoteBackupIsNewer(remote, baseline) {
		t.Fatal("newer remote version was not reported")
	}
	baseline = &ServiceBaseline{BackupID: "bkp_new", CreatedAt: "2026-07-31T12:00:00Z"}
	if remoteBackupIsNewer(remote, baseline) {
		t.Fatal("the known remote version should not be reported as newer")
	}
	baseline = &ServiceBaseline{BackupID: "bkp_future", CreatedAt: "2026-07-31T13:00:00Z"}
	if remoteBackupIsNewer(remote, baseline) {
		t.Fatal("an older remote version should not be reported as newer")
	}
}
