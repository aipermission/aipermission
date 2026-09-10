package uisession

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManagerIssuesScopedSecureDatabaseBoundSession(t *testing.T) {
	manager := New("3212")
	response := httptest.NewRecorder()
	if err := manager.Issue(response, "workspace-a", "retry-a"); err != nil {
		t.Fatal(err)
	}
	cookies := cookieMap(response.Result().Cookies())
	session := cookies[SessionCookieBase+"_3212"]
	csrf := cookies[CSRFCookieBase+"_3212"]
	workspace := cookies[WorkspaceCookieBase+"_3212"]
	if session == nil || csrf == nil || workspace == nil {
		t.Fatalf("cookies = %#v", cookies)
	}
	for _, cookie := range cookies {
		if !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
			t.Fatalf("insecure cookie = %#v", cookie)
		}
	}
	if !session.HttpOnly || csrf.HttpOnly || workspace.Value != RetryIdentity("retry-a") {
		t.Fatalf("cookie boundaries session=%#v csrf=%#v workspace=%#v", session, csrf, workspace)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/connectors", nil)
	request.AddCookie(session)
	if !manager.Valid(request, "workspace-a") || manager.Valid(request, "workspace-b") {
		t.Fatal("session was not bound to its workspace")
	}
	request.AddCookie(csrf)
	request.Header.Set(CSRFHeaderName, csrf.Value)
	if !manager.ValidCSRF(request) {
		t.Fatal("matching CSRF cookie and header were rejected")
	}
	request.Header.Set(CSRFHeaderName, "different")
	if manager.ValidCSRF(request) {
		t.Fatal("mismatched CSRF header was accepted")
	}
}

func TestManagerExpiresAndClearsSessions(t *testing.T) {
	now := time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC)
	manager := New("")
	manager.now = func() time.Time { return now }
	prepared, err := Prepare()
	if err != nil {
		t.Fatal(err)
	}
	prepared.expires = now.Add(time.Minute)
	response := httptest.NewRecorder()
	if err := manager.IssuePrepared(response, prepared, "workspace", "retry"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.AddCookie(cookieMap(response.Result().Cookies())[SessionCookieBase])
	if !manager.Valid(request, "workspace") {
		t.Fatal("fresh session was rejected")
	}
	now = now.Add(2 * time.Minute)
	if manager.Valid(request, "workspace") {
		t.Fatal("expired session was accepted")
	}
	clearResponse := httptest.NewRecorder()
	manager.Clear(clearResponse)
	for _, cookie := range clearResponse.Result().Cookies() {
		if !cookie.Secure || cookie.MaxAge >= 0 {
			t.Fatalf("invalid clearing cookie = %#v", cookie)
		}
	}
}

func TestManagerRejectsInvalidPreparedSession(t *testing.T) {
	manager := New("")
	response := httptest.NewRecorder()
	if err := manager.IssuePrepared(response, Prepared{}, "workspace", "retry"); !errors.Is(err, ErrInvalidPrepared) {
		t.Fatalf("zero prepared session error = %v", err)
	}
	if len(response.Result().Cookies()) != 0 {
		t.Fatal("invalid prepared session emitted cookies")
	}
}

func TestWorkspaceCookieRefreshAndRouteExemptions(t *testing.T) {
	manager := New("3210")
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.AddCookie(&http.Cookie{Name: WorkspaceCookieBase + "_3210", Value: "stale"})
	response := httptest.NewRecorder()
	manager.EnsureWorkspaceCookie(response, request, "current")
	cookie := cookieMap(response.Result().Cookies())[WorkspaceCookieBase+"_3210"]
	if cookie == nil || cookie.Value != RetryIdentity("current") {
		t.Fatalf("workspace cookie = %#v", cookie)
	}
	for _, path := range []string{"/health", "/api/status", "/api/unlock/status", "/api/unlock", "/api/mcp/connector-help"} {
		if !IsExempt(path) {
			t.Fatalf("expected exempt path %q", path)
		}
	}
	if IsExempt("/api/connectors") || IsExempt("/api/unlock/setup") {
		t.Fatal("protected route was marked UI-session exempt")
	}
}

func cookieMap(cookies []*http.Cookie) map[string]*http.Cookie {
	result := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		result[cookie.Name] = cookie
	}
	return result
}
