package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestImportCommitRevalidatesRevokedUISession(t *testing.T) {
	server := newLockedAPITestServer(t)
	defer server.Close()
	setup := performJSON(server.Handler(), http.MethodPost, "/api/unlock/setup", "", setupUnlockRequest{
		Password: "WorkspacePassword123", ConfirmPassword: "WorkspacePassword123",
	})
	if setup.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", setup.Code, setup.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/backup/import", nil)
	attachTestUIAuthorization(request)
	response := httptest.NewRecorder()
	check, ok := server.authorizeBackupImport(response, request)
	if !ok || check == nil {
		t.Fatal("valid import admission was rejected")
	}
	server.clearUISessions(httptest.NewRecorder())
	if check() || response.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session authorized import commit: %d %s", response.Code, response.Body.String())
	}
}
