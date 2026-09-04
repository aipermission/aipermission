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
	srv.mu.Lock()
	err := srv.issueUISessionLocked(issueResponse)
	srv.mu.Unlock()
	if err != nil {
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
		workspaces:     map[string]*databaseRuntime{"default": {uiRetryIdentity: "retry-one"}},
		uiSessions:     map[string]uiSessionRecord{},
	}
	second := &Server{
		config:         config.Config{FrontendPort: "3212"},
		activeDatabase: "default",
		workspaces:     map[string]*databaseRuntime{"default": {uiRetryIdentity: "retry-two"}},
		uiSessions:     map[string]uiSessionRecord{},
	}

	firstResponse := httptest.NewRecorder()
	first.mu.Lock()
	err := first.issueUISessionLocked(firstResponse)
	first.mu.Unlock()
	if err != nil {
		t.Fatalf("issue first ui session: %v", err)
	}
	secondResponse := httptest.NewRecorder()
	second.mu.Lock()
	err = second.issueUISessionLocked(secondResponse)
	second.mu.Unlock()
	if err != nil {
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
	if firstCookies["aipermission_workspace_3210"].Value != uiRetryIdentity("retry-one") || secondCookies["aipermission_workspace_3212"].Value != uiRetryIdentity("retry-two") {
		t.Fatalf("expected workspace-bound cookies, got first=%v second=%v", firstCookies, secondCookies)
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

func TestEnsureUIWorkspaceCookieReplacesStaleDatabaseIdentity(t *testing.T) {
	srv := &Server{
		config:         config.Config{FrontendPort: "3212"},
		activeDatabase: "second",
		workspaces:     map[string]*databaseRuntime{"second": {uiRetryIdentity: "current-retry"}},
	}
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.AddCookie(&http.Cookie{Name: "aipermission_workspace_3212", Value: "old-workspace"})
	recorder := httptest.NewRecorder()
	srv.ensureUIWorkspaceCookie(recorder, request)
	cookie := cookiesByName(recorder.Result().Cookies())["aipermission_workspace_3212"]
	if cookie == nil || cookie.Value != uiRetryIdentity("current-retry") {
		t.Fatalf("workspace cookie=%v, want current workspace", cookie)
	}
}

func TestUIRetryIdentitySeparatesRestoredDatabaseCopies(t *testing.T) {
	original := uiRetryIdentity("retry-instance-one")
	copy := uiRetryIdentity("retry-instance-two")
	if original == "" || copy == "" || original == copy {
		t.Fatalf("retry identities must differ across database instances: original=%q copy=%q", original, copy)
	}
}

func TestUIRetryIdentitySurvivesDatabaseRename(t *testing.T) {
	before := uiRetryIdentity("stable-retry-instance")
	after := uiRetryIdentity("stable-retry-instance")
	if before == "" || before != after {
		t.Fatalf("database rename changed retry identity: before=%q after=%q", before, after)
	}
}

func cookiesByName(cookies []*http.Cookie) map[string]*http.Cookie {
	result := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		result[cookie.Name] = cookie
	}
	return result
}
