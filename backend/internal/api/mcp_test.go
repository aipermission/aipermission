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
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
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
	tokenStore := tokens.NewStore(database)
	srv := NewServer(config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       filepath.Join(t.TempDir(), "aipermission.db"),
		GatewaySecret:  "gateway-secret",
		AllowedOrigins: []string{"http://localhost:3001"},
	}, database, secretVault, tokenStore, WithConnectorRegistry(testConnectorRegistry(t)))
	sshKeyStore := testSSHKeyStore(t, srv.activeRuntime())
	srv.activeRuntime().setMCPStarted(true)
	authorizeTestUISession(srv)
	t.Cleanup(func() {
		srv.Close()
		_ = database.Close()
	})
	return apiTestFixture{server: srv, db: database, tokens: tokenStore, sshKeys: sshKeyStore}
}

func testSSHKeyStore(t *testing.T, runtime *databaseRuntime) *sshkeys.Store {
	t.Helper()
	store, ok := runtime.ConnectorResource("ssh", "keys").(*sshkeys.Store)
	if !ok || store == nil {
		t.Fatalf("ssh key resource store is not available")
	}
	return store
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
		TargetRef: connectortargets.TargetProfileRef("ssh", target.ID, profile.ID),
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

func TestMCPConnectorTargetsExposeMetadataOnlyWhenEnabled(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	profile := fixture.createKeyAndServer(t, "core-1")
	store := connectortargets.NewStore(fixture.db)
	if err := store.SetActionPermission(ctx, connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      profile.TargetID,
		ProfileID:     profile.ProfileID,
		ActionName:    sshconnector.ActionExec,
		ExecutionRule: connectortargets.ActionPermissionAlwaysRun,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list connector targets failed: %d %s", response.Code, response.Body.String())
	}
	var items []mcpConnectorTargetItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode connector targets: %v", err)
	}
	if len(items) != 1 || len(items[0].Metadata) != 0 {
		t.Fatalf("metadata should be hidden by default: %#v", items)
	}

	settingsResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/security", "", updateSecuritySettingsRequest{ExposeMCPServerMetadata: true})
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("enable metadata setting failed: %d %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-targets", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list connector targets with metadata failed: %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode connector targets with metadata: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one connector target, got %#v", items)
	}
	metadata := items[0].Metadata
	if metadata["host"] != profile.Host || metadata["username"] != profile.Username || int64ConfigValue(metadata, "port") != int64(profile.Port) {
		t.Fatalf("unexpected exposed metadata: %#v", metadata)
	}
	if _, ok := metadata["ssh_key_id"]; ok {
		t.Fatalf("metadata should not expose credential ids: %#v", metadata)
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
		{method: http.MethodPost, path: "/api/mcp/exec", body: map[string]any{"runtime_profile_id": 1, "command": "date"}},
		{method: http.MethodGet, path: "/api/mcp/requests"},
		{method: http.MethodGet, path: "/api/mcp/console?runtime_profile_id=1"},
		{method: http.MethodGet, path: "/api/mcp/file-transfers"},
	} {
		response := performJSON(fixture.server.Handler(), tc.method, tc.path, token.TokenValue, tc.body)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s %s should be removed, got %d", tc.method, tc.path, response.Code)
		}
	}
}
