package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/servers"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

type apiTestFixture struct {
	server  *Server
	db      *sql.DB
	tokens  *tokens.Store
	servers *servers.Store
	sshKeys *sshkeys.Store
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
	serverStore := servers.NewStore(database)
	sshKeyStore := sshkeys.NewStore(database, secretVault)
	tokenStore := tokens.NewStore(database)
	srv := NewServer(config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       filepath.Join(t.TempDir(), "aipermission.db"),
		GatewaySecret:  "gateway-secret",
		AllowedOrigins: []string{"http://localhost:3001"},
	}, database, secretVault, serverStore, sshKeyStore, tokenStore)
	srv.activeRuntime().setMCPStarted(true)
	authorizeTestUISession(srv)
	t.Cleanup(func() {
		srv.Close()
		_ = database.Close()
	})
	return apiTestFixture{server: srv, db: database, tokens: tokenStore, servers: serverStore, sshKeys: sshKeyStore}
}

func (f apiTestFixture) createKeyAndServer(t *testing.T, name string) servers.Server {
	t.Helper()
	key, err := f.sshKeys.Create(context.Background(), sshkeys.CreateRequest{Name: name + "-key", KeyType: sshkeys.TypeED25519})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}
	server, err := f.servers.Create(context.Background(), servers.CreateRequest{
		Name:        name,
		Host:        "127.0.0.1",
		Port:        22,
		Username:    "root",
		SSHKeyID:    key.ID,
		Description: "description for " + name,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	return server
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

func TestMCPListServersUsesTokenPermissionsAndHidesBlocked(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	allowed := fixture.createKeyAndServer(t, "allowed")
	blocked := fixture.createKeyAndServer(t, "blocked")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: allowed.ID, ExecutionRule: tokens.RuleAlwaysRun},
		{ServerID: blocked.ID, ExecutionRule: tokens.RuleBlocked},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var items []mcpServerItem
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(items) != 1 || items[0].ID != allowed.ID || items[0].ExecutionRule != tokens.RuleAlwaysRun {
		t.Fatalf("unexpected mcp servers: %#v", items)
	}
	if items[0].Description != allowed.Description {
		t.Fatalf("expected description in mcp server list, got %#v", items[0])
	}
	if items[0].Host != "" || items[0].Port != 0 || items[0].Username != "" {
		t.Fatalf("endpoint metadata should be hidden by default: %#v", items[0])
	}
	if len(items[0].Hints) == 0 || !strings.Contains(strings.Join(items[0].Hints, " "), "hash -r") {
		t.Fatalf("expected command hygiene hints in mcp server list: %#v", items[0].Hints)
	}

	if err := writeSecuritySettings(ctx, fixture.server.activeRuntime(), securitySettingsResponse{
		ExposeMCPServerMetadata: true,
		RedactionMode:           redactionModeBasic,
	}); err != nil {
		t.Fatalf("write security settings: %v", err)
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200 with metadata enabled, got %d: %s", response.Code, response.Body.String())
	}
	items = nil
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode metadata response: %v", err)
	}
	if len(items) != 1 || items[0].Host == "" || items[0].Port == 0 || items[0].Username == "" {
		t.Fatalf("endpoint metadata should be present when enabled: %#v", items)
	}

	if _, err := fixture.tokens.Revoke(ctx, token.ID); err != nil {
		t.Fatalf("revoke token: %v", err)
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", token.TokenValue, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected revoked token to be unauthorized, got %d", response.Code)
	}
}

func TestMCPAuthenticationRejectsDuplicateTokenAcrossUnlockedDatabases(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}

	cloneDB, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "clone.db"), "test-password")
	if err != nil {
		t.Fatalf("open clone db: %v", err)
	}
	defer cloneDB.Close()
	if _, err := cloneDB.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('agent-clone', ?, ?, datetime('now'), datetime('now'))`,
		tokens.HashToken(token.TokenValue),
		token.TokenPrefix,
	); err != nil {
		t.Fatalf("insert duplicate token: %v", err)
	}

	fixture.server.mu.Lock()
	fixture.server.workspaces["clone"] = &databaseRuntime{id: "clone", path: "clone.db", gatewaySecret: "gateway-secret", database: cloneDB}
	fixture.server.mu.Unlock()

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", token.TokenValue, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected duplicate token conflict, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "multiple unlocked databases") {
		t.Fatalf("expected duplicate workspace guidance, got %s", response.Body.String())
	}
}

func TestMCPAuthenticationIgnoresStoredTokenValueFallback(t *testing.T) {
	fixture := newAPITestFixture(t)
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, token_value, created_at, updated_at)
		VALUES ('agent', ?, 'real-tok', 'legacy-token-value', ?, ?)`,
		tokens.HashToken("real-token-value"),
		now,
		now,
	); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", "legacy-token-value", nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("stored token_value must not authenticate MCP, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "invalid, revoked, or expired API token") || strings.Contains(response.Body.String(), "prefix") {
		t.Fatalf("invalid token response should be generic, got %s", response.Body.String())
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", "real-token-value", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("token_hash should authenticate MCP, got %d: %s", response.Code, response.Body.String())
	}
}

func TestMCPAuthenticationRejectsExpiredToken(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{
		Name:      "short-lived",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `UPDATE api_tokens SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), token.ID); err != nil {
		t.Fatalf("expire token: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", token.TokenValue, nil)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected expired token to be unauthorized, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "invalid, revoked, or expired API token") {
		t.Fatalf("expired token response should be generic, got %s", response.Body.String())
	}
}

func TestMCPExecApprovalPendingCreatesAuditableRequestAndConsumesUserNote(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	runtime := fixture.server.activeRuntime()
	note := "check disk before install"
	if _, err := fixture.server.insertMessage(ctx, runtime, createMessageRequest{TokenID: token.ID, ServerID: &server.ID, Message: note}); err != nil {
		t.Fatalf("insert message: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, map[string]any{
		"server_id": server.ID,
		"command":   "df -h",
		"reason":    "investigate storage",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body mcpExecResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode exec response: %v", err)
	}
	if body.Status != "approval_pending" || body.RequestID == 0 || body.AssistantHint == "" || body.RetryAfterSeconds != 3 {
		t.Fatalf("unexpected approval response: %#v", body)
	}
	if body.UserNote == nil || *body.UserNote != note {
		t.Fatalf("expected user note to be consumed, got %#v", body.UserNote)
	}

	record, err := fixture.server.getCommandRequest(ctx, runtime, body.RequestID, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if record.Command != "df -h" || record.Reason != "investigate storage" || record.Status != "pending_approval" {
		t.Fatalf("unexpected command request: %#v", record)
	}
	nextNote, err := fixture.server.consumeNextUserMessage(ctx, runtime, token.ID, server.ID, 0)
	if err != nil {
		t.Fatalf("consume next note: %v", err)
	}
	if nextNote != nil {
		t.Fatalf("expected first MCP response to consume the only note, got %#v", nextNote)
	}
}

func TestMCPRuntimeSwitchBlocksExecutionWithoutDeletingPermissions(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleAlwaysRun},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}

	if response := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/mcp-runtime", "", updateMCPRuntimeRequest{Enabled: false}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":false`) {
		t.Fatalf("stop MCP runtime failed: %d %s", response.Code, response.Body.String())
	}
	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, map[string]any{
		"server_id": server.ID,
		"command":   "ls",
	})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"stopped"`) {
		t.Fatalf("stopped MCP runtime should block exec without changing permissions, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/permissions", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), tokens.RuleAlwaysRun) {
		t.Fatalf("stopping MCP runtime should not delete permissions: %d %s", response.Code, response.Body.String())
	}

	if response := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/mcp-runtime", "", updateMCPRuntimeRequest{Enabled: true}); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"enabled":true`) {
		t.Fatalf("start MCP runtime failed: %d %s", response.Code, response.Body.String())
	}
	response = performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, map[string]any{
		"server_id": server.ID,
		"command":   "echo ok",
	})
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), `"status":"stopped"`) {
		t.Fatalf("started MCP runtime should allow exec path, got %d %s", response.Code, response.Body.String())
	}
}

func TestMCPExecDoesNotAttachNewRequestToExistingActiveCommand(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleAlwaysRun},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}

	runtime := fixture.server.activeRuntime()
	runtime.consoleSessions.SeedActiveCommandForTest(99, server.ID, "sleep 60", "active output\n")

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, map[string]any{
		"server_id": server.ID,
		"command":   "docker ps",
		"reason":    "inspect containers",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body mcpExecResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode exec response: %v", err)
	}
	if body.Status != "error" || body.RequestID == 0 || body.Command != "docker ps" {
		t.Fatalf("new request should fail independently, got %#v", body)
	}
	if !strings.Contains(body.Error, "another command is already running") {
		t.Fatalf("expected active command guidance, got %q", body.Error)
	}
	record, err := fixture.server.getCommandRequest(ctx, runtime, body.RequestID, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if record.Command != "docker ps" || record.Status != "error" || record.Error == "" {
		t.Fatalf("new request should not attach to the previous active command: %#v", record)
	}
}

func TestMCPExecValidatesInputAndBlocksMissingPermission(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")

	cases := []struct {
		name string
		body map[string]any
		code int
	}{
		{name: "missing server", body: map[string]any{"command": "ls"}, code: http.StatusBadRequest},
		{name: "missing command", body: map[string]any{"server_id": server.ID, "command": " "}, code: http.StatusBadRequest},
	}
	for _, tc := range cases {
		response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, tc.body)
		if response.Code != tc.code {
			t.Fatalf("%s: expected %d, got %d", tc.name, tc.code, response.Code)
		}
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, map[string]any{
		"server_id": server.ID,
		"command":   "ls",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	if !strings.Contains(response.Body.String(), `"status":"blocked"`) {
		t.Fatalf("expected blocked response, got %s", response.Body.String())
	}
}
