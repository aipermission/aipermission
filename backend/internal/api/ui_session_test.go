package api

import (
	"net/http/httptest"
	"testing"
)

func TestUISessionCookiesUseSecureLocalBoundary(t *testing.T) {
	srv := &Server{
		activeDatabase: "default",
		uiSessions:     map[string]uiSessionRecord{},
	}

	issueResponse := httptest.NewRecorder()
	if err := srv.issueUISession(issueResponse); err != nil {
		t.Fatalf("issue ui session: %v", err)
	}
	for _, cookie := range issueResponse.Result().Cookies() {
		if !cookie.Secure {
			t.Fatalf("issued cookie %s should use Secure flag", cookie.Name)
		}
	}

	clearResponse := httptest.NewRecorder()
	srv.clearUISessions(clearResponse)
	for _, cookie := range clearResponse.Result().Cookies() {
		if !cookie.Secure {
			t.Fatalf("cleared cookie %s should use Secure flag", cookie.Name)
		}
	}
}
