package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apihttp "github.com/aipermission/aipermission/backend/internal/api/httptransport"
)

func TestBackupAuthorizerRejectsStaleAndValidHeadRequests(t *testing.T) {
	server := uiSessionTestServer(t, "3212", "current", "current-retry")
	issued := httptest.NewRecorder()
	if err := server.issueUISessionLocked(issued); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/backup/download", "/api/backup/providers/3/records/9/download"} {
		for _, binding := range []string{"", "workspace-a", server.currentUIWorkspaceBinding()} {
			request := httptest.NewRequest(http.MethodHead, path, nil)
			for _, cookie := range issued.Result().Cookies() {
				request.AddCookie(cookie)
			}
			request.Header.Set(apihttp.WorkspaceHeaderName, binding)
			response := httptest.NewRecorder()
			wantStatus := http.StatusConflict
			if binding == server.currentUIWorkspaceBinding() {
				wantStatus = http.StatusMethodNotAllowed
			}
			if server.authorizeBackupOperation(response, request) || response.Code != wantStatus {
				t.Fatalf("backup %s HEAD binding %q authorized: status %d want %d", path, binding, response.Code, wantStatus)
			}
			if wantStatus == http.StatusMethodNotAllowed && (response.Header().Get("Allow") != http.MethodGet || response.Body.Len() != 0) {
				t.Fatal("secondary backup authorizer did not return bodyless GET-only rejection")
			}
		}
	}
}
