package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/config"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type apiTestFixture struct {
	server  *Server
	db      *sql.DB
	tokens  *tokens.Store
	sshKeys *sshkeys.Store
}

type testSSHConnectorProfile struct {
	ID        int64
	TargetID  int64
	ProfileID int64
	Name      string
	Host      string
	Port      int
	Username  string
	SSHKeyID  int64
	TargetRef string
}

const testUISessionToken = "test-ui-session"
const testUICSRFToken = "test-ui-csrf"

var (
	testUICookieMu sync.Mutex
	testUICookie   *http.Cookie
)

func newAPITestFixture(t *testing.T) apiTestFixture {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	secretVault, err := vault.New("gateway-secret")
	if err != nil {
		t.Fatalf("new vault: %v", err)
	}
	sshKeyStore := sshkeys.NewStore(database, secretVault)
	tokenStore := tokens.NewStore(database)
	srv := NewServer(config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       filepath.Join(t.TempDir(), "aipermission.db"),
		GatewaySecret:  "gateway-secret",
		AllowedOrigins: []string{"http://localhost:3001"},
	}, database, secretVault, sshKeyStore, tokenStore)
	srv.activeRuntime().setMCPStarted(true)
	authorizeTestUISession(srv)
	t.Cleanup(func() {
		srv.Close()
		_ = database.Close()
	})
	return apiTestFixture{server: srv, db: database, tokens: tokenStore, sshKeys: sshKeyStore}
}

func (f apiTestFixture) createKeyAndServer(t *testing.T, name string) testSSHConnectorProfile {
	t.Helper()
	return createTestSSHConnectorProfile(t, f.db, f.sshKeys, name)
}

func createTestSSHConnectorProfile(t *testing.T, database *sql.DB, sshKeyStore *sshkeys.Store, name string) testSSHConnectorProfile {
	t.Helper()
	key, err := sshKeyStore.Create(context.Background(), sshkeys.CreateRequest{Name: name + "-key", KeyType: sshkeys.TypeED25519})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	store := connectortargets.NewStore(database)
	target, err := store.CreateTarget(context.Background(), connectortargets.CreateTargetInput{
		ConnectorKind: "ssh",
		Name:          name,
		Config: map[string]any{
			"host":        "127.0.0.1",
			"port":        22,
			"description": "description for " + name,
		},
	})
	if err != nil {
		t.Fatalf("create connector target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(context.Background(), connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: "ssh",
		Kind:          "private_key",
		Label:         "root",
		Public: map[string]any{
			"username":    "root",
			"ssh_key_id":  key.ID,
			"key_name":    key.Name,
			"key_type":    key.KeyType,
			"fingerprint": key.Fingerprint,
		},
	})
	if err != nil {
		t.Fatalf("create connector profile: %v", err)
	}
	return testSSHConnectorProfile{
		ID:        profile.ID,
		TargetID:  target.ID,
		ProfileID: profile.ID,
		Name:      name,
		Host:      "127.0.0.1",
		Port:      22,
		Username:  "root",
		SSHKeyID:  key.ID,
		TargetRef: connectortargets.SSHTargetRef(target.ID, profile.ID),
	}
}

func performJSON(handler http.Handler, method string, path string, token string, body any) *httptest.ResponseRecorder {
	return performJSONWithOptions(handler, method, path, token, body, true)
}

func performJSONWithoutUICookie(handler http.Handler, method string, path string, token string, body any) *httptest.ResponseRecorder {
	return performJSONWithOptions(handler, method, path, token, body, false)
}

func performJSONWithOptions(handler http.Handler, method string, path string, token string, body any, includeUICookie bool) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		payload, _ := json.Marshal(body)
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Host = "localhost:8080"
	request.RemoteAddr = "127.0.0.1:12345"
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("X-API-Key", token)
	} else if includeUICookie {
		if cookie := currentTestUICookie(); cookie != nil {
			request.AddCookie(cookie)
		}
		request.AddCookie(&http.Cookie{Name: uiCSRFCookieName, Value: testUICSRFToken})
		request.AddCookie(&http.Cookie{Name: uiCSRFCookieName, Value: testUICSRFToken})
		request.Header.Set(uiCSRFHeaderName, testUICSRFToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	recordTestUICookies(response.Result().Cookies())
	return response
}

func authorizeTestUISession(srv *Server) {
	expires := time.Now().UTC().Add(uiSessionMaxAge)
	srv.uiSessionMu.Lock()
	if srv.uiSessions == nil {
		srv.uiSessions = map[string]uiSessionRecord{}
	}
	srv.uiSessions[hashUISessionToken(testUISessionToken)] = uiSessionRecord{Expires: expires, DatabaseID: srv.activeDatabase}
	srv.uiSessionMu.Unlock()
	recordTestUICookies([]*http.Cookie{{
		Name:    uiSessionCookieName,
		Value:   testUISessionToken,
		Path:    "/",
		Expires: expires,
		MaxAge:  int(uiSessionMaxAge.Seconds()),
	}})
}

func currentTestUICookie() *http.Cookie {
	testUICookieMu.Lock()
	defer testUICookieMu.Unlock()
	if testUICookie == nil {
		return nil
	}
	copy := *testUICookie
	return &copy
}

func recordTestUICookies(cookies []*http.Cookie) {
	testUICookieMu.Lock()
	defer testUICookieMu.Unlock()
	for _, cookie := range cookies {
		if cookie.Name != uiSessionCookieName {
			continue
		}
		if cookie.MaxAge < 0 || cookie.Value == "" {
			testUICookie = nil
			continue
		}
		copy := *cookie
		testUICookie = &copy
	}
}

func TestMCPConnectorTargetsRequireValidToken(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var items []mcpConnectorTargetItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("new token should not see connector targets without permissions: %#v", items)
	}

	if _, err := fixture.tokens.Revoke(ctx, token.ID); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token to be unauthorized, got %d", response.Code)
	}
}

func TestMCPConnectorActionsOnlyExposeGrantedActions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionGetSchemas,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("set blocked connector permission: %v", err)
	}

	targetRef := connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID)
	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-actions?target_ref="+url.QueryEscape(targetRef), token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("get connector actions failed: %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	if !strings.Contains(body, postgresconnector.ActionGetSchemas) {
		t.Fatalf("granted action missing from response: %s", body)
	}
	if strings.Contains(body, postgresconnector.ActionQueryReadonly) || strings.Contains(body, postgresconnector.ActionDescribeTable) {
		t.Fatalf("ungranted action leaked through MCP action list: %s", body)
	}
}

func TestOldMCPSSHWrapperRoutesAreNotRegistered(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	for _, tc := range []struct {
		method string
		path   string
		body   any
	}{
		{method: http.MethodGet, path: "/api/mcp/servers"},
		{method: http.MethodPost, path: "/api/mcp/exec", body: map[string]any{"server_id": 1, "command": "date"}},
		{method: http.MethodGet, path: "/api/mcp/requests"},
		{method: http.MethodGet, path: "/api/mcp/console?server_id=1"},
		{method: http.MethodGet, path: "/api/mcp/file-transfers"},
	} {
		response := performJSON(fixture.server.Handler(), tc.method, tc.path, token.TokenValue, tc.body)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s should be removed, got %d", tc.method, tc.path, response.Code)
		}
	}
}
