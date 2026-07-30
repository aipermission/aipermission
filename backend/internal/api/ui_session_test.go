package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
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

func TestUISessionCookiesAreScopedByFrontendPort(t *testing.T) {
	first := &Server{
		config:         config.Config{FrontendPort: "3210"},
		activeDatabase: "default",
		uiSessions:     map[string]uiSessionRecord{},
	}
	second := &Server{
		config:         config.Config{FrontendPort: "3212"},
		activeDatabase: "default",
		uiSessions:     map[string]uiSessionRecord{},
	}

	firstResponse := httptest.NewRecorder()
	if err := first.issueUISession(firstResponse); err != nil {
		t.Fatalf("issue first ui session: %v", err)
	}
	secondResponse := httptest.NewRecorder()
	if err := second.issueUISession(secondResponse); err != nil {
		t.Fatalf("issue second ui session: %v", err)
	}

	firstCookies := cookiesByName(firstResponse.Result().Cookies())
	secondCookies := cookiesByName(secondResponse.Result().Cookies())
	firstSession := firstCookies["aipermission_ui_session_3210"]
	secondSession := secondCookies["aipermission_ui_session_3212"]
	if firstSession == nil || secondSession == nil {
		t.Fatalf("expected port-scoped session cookies, got first=%v second=%v", firstCookies, secondCookies)
	}
	if firstCookies["aipermission_csrf_3210"] == nil || secondCookies["aipermission_csrf_3212"] == nil {
		t.Fatalf("expected port-scoped csrf cookies, got first=%v second=%v", firstCookies, secondCookies)
	}

	firstRequest := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	firstRequest.AddCookie(firstSession)
	firstRequest.AddCookie(secondSession)
	if !first.hasValidUISession(firstRequest) {
		t.Fatal("first instance should accept its own session cookie")
	}
	if !second.hasValidUISession(firstRequest) {
		t.Fatal("second instance should accept its own session cookie without displacing the first")
	}

	firstCSRF := firstCookies["aipermission_csrf_3210"]
	secondCSRF := secondCookies["aipermission_csrf_3212"]
	firstMutation := httptest.NewRequest(http.MethodPost, "/api/tokens", nil)
	firstMutation.AddCookie(firstCSRF)
	firstMutation.AddCookie(secondCSRF)
	firstMutation.Header.Set(uiCSRFHeaderName, firstCSRF.Value)
	if !first.hasValidUICSRF(firstMutation) {
		t.Fatal("first instance should accept its own csrf cookie")
	}
	if second.hasValidUICSRF(firstMutation) {
		t.Fatal("second instance should reject the first instance csrf header")
	}

	secondMutation := httptest.NewRequest(http.MethodPost, "/api/tokens", nil)
	secondMutation.AddCookie(firstCSRF)
	secondMutation.AddCookie(secondCSRF)
	secondMutation.Header.Set(uiCSRFHeaderName, secondCSRF.Value)
	if !second.hasValidUICSRF(secondMutation) {
		t.Fatal("second instance should accept its own csrf cookie")
	}
}

func cookiesByName(cookies []*http.Cookie) map[string]*http.Cookie {
	result := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		result[cookie.Name] = cookie
	}
	return result
}
