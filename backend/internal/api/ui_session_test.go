package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	apihttp "github.com/aipermission/aipermission/backend/internal/api/httptransport"
	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/databasecatalog"
	gatewayaccess "github.com/aipermission/aipermission/backend/internal/gatewayaccess"
	connectorapi "github.com/aipermission/aipermission/backend/internal/gatewayconnectorapi"
	gatewayinfra "github.com/aipermission/aipermission/backend/internal/gatewayinfrastructure"
)

func uiSessionTestServer(t *testing.T, port, databaseID, retryIdentity string) *Server {
	t.Helper()
	configuration := snapshotRuntimeConfiguration(config.Config{FrontendPort: port})
	infrastructure := gatewayinfra.NewComponent(configuration.DataPath, describeDatabaseRuntime)
	server := &Server{
		config: configuration, access: gatewayaccess.NewComponent(port),
	}
	server.bindInfrastructure(infrastructure)
	if retryIdentity != "" {
		database := openAPITestDB(t)
		if _, err := database.Exec(`UPDATE settings SET value = ? WHERE key = 'workspace_uuid'`, databaseID); err != nil {
			t.Fatal(err)
		}
		path := testDatabasePath(t, database)
		runtime, err := server.workspaceOwner.OpenWorkspace(t.Context(), gatewayinfra.NewOpenWorkspaceInput(
			databaseID, path, "test-password", "test-password", testConnectorRegistry(t), connectorapi.NewRegistry(),
		))
		if err != nil {
			t.Fatal(err)
		}
		server.workspaceOwner.ActivateWorkspace(runtime)
		t.Cleanup(func() { _ = server.workspaceOwner.DiscardWorkspace(runtime, nil, nil) })
	}
	return server
}

func TestUISessionCookiesUseSecureLocalBoundary(t *testing.T) {
	srv := uiSessionTestServer(t, "", databasecatalog.DefaultDatabaseID(""), "")

	issueResponse := httptest.NewRecorder()
	err := srv.issueUISessionLocked(issueResponse)
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
	first := uiSessionTestServer(t, "3210", "default", "retry-one")
	second := uiSessionTestServer(t, "3212", "default", "retry-two")

	firstResponse := httptest.NewRecorder()
	err := first.issueUISessionLocked(firstResponse)
	if err != nil {
		t.Fatalf("issue first ui session: %v", err)
	}
	secondResponse := httptest.NewRecorder()
	err = second.issueUISessionLocked(secondResponse)
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
	firstWorkspace := firstCookies["aipermission_workspace_3210"]
	secondWorkspace := secondCookies["aipermission_workspace_3212"]
	if firstWorkspace == nil || secondWorkspace == nil || firstWorkspace.Value == "" || secondWorkspace.Value == "" || firstWorkspace.Value == secondWorkspace.Value {
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
	firstRequest.URL.Path = "/api/backup/download"
	firstRequest.Header.Set(apihttp.WorkspaceHeaderName, secondWorkspace.Value)
	staleDownload := httptest.NewRecorder()
	if first.authorizeBackupOperation(staleDownload, firstRequest) || staleDownload.Code != http.StatusConflict {
		t.Fatalf("stale download authorization = %d, want %d", staleDownload.Code, http.StatusConflict)
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
	srv := uiSessionTestServer(t, "3212", "second", "current-retry")
	request := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	request.AddCookie(&http.Cookie{Name: "aipermission_workspace_3212", Value: "old-workspace"})
	recorder := httptest.NewRecorder()
	srv.ensureUIWorkspaceCookie(recorder, request)
	cookie := cookiesByName(recorder.Result().Cookies())["aipermission_workspace_3212"]
	if cookie == nil || cookie.Value == "" || cookie.Value == "old-workspace" {
		t.Fatalf("workspace cookie=%v, want current workspace", cookie)
	}
}

func cookiesByName(cookies []*http.Cookie) map[string]*http.Cookie {
	result := make(map[string]*http.Cookie, len(cookies))
	for _, cookie := range cookies {
		result[cookie.Name] = cookie
	}
	return result
}
