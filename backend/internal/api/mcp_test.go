package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/config"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
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
	expired := fixture.createKeyAndServer(t, "expired")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: allowed.ID, ExecutionRule: tokens.RuleAlwaysRun},
		{ServerID: blocked.ID, ExecutionRule: tokens.RuleBlocked},
		{ServerID: expired.ID, ExecutionRule: tokens.RuleAlwaysRun, ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339)},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `UPDATE token_server_permissions SET expires_at = ? WHERE token_id = ? AND server_id = ?`, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), token.ID, expired.ID); err != nil {
		t.Fatalf("expire permission: %v", err)
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
	if items[0].LiveConsoleStatus != "none" || items[0].LastConsoleError != "" {
		t.Fatalf("expected no live console context by default: %#v", items[0])
	}
	if len(items[0].Hints) == 0 || !strings.Contains(strings.Join(items[0].Hints, " "), "hash -r") {
		t.Fatalf("expected command hygiene hints in mcp server list: %#v", items[0].Hints)
	}
	if !strings.Contains(strings.Join(items[0].Hints, " "), "permission-scoped") {
		t.Fatalf("expected permission-scoped health hint in mcp server list: %#v", items[0].Hints)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := fixture.db.ExecContext(ctx, `
		INSERT INTO console_sessions (server_id, name, status, transcript, error, cols, rows, created_at, updated_at)
		VALUES (?, 'failed-ssh', 'error', '', 'dial tcp 203.0.113.10:22: i/o timeout', 120, 32, ?, ?)`,
		allowed.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert console context: %v", err)
	}
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/servers", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200 after console context, got %d: %s", response.Code, response.Body.String())
	}
	items = nil
	if err := json.Unmarshal(response.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode console context response: %v", err)
	}
	if len(items) != 1 || items[0].LiveConsoleStatus != "error" || !strings.Contains(items[0].LastConsoleError, "i/o timeout") {
		t.Fatalf("expected last known console error context: %#v", items)
	}
	expiredResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/exec", token.TokenValue, map[string]any{
		"server_id": expired.ID,
		"command":   "date",
		"reason":    "verify expired grant",
	})
	if expiredResponse.Code != http.StatusOK || !strings.Contains(expiredResponse.Body.String(), `"status":"blocked"`) {
		t.Fatalf("expired permission should not allow execution, got %d %s", expiredResponse.Code, expiredResponse.Body.String())
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

func TestApprovalRunRejectsServerContextDriftBeforeExecution(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "core")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}

	id, err := fixture.server.insertCommandRequest(ctx, fixture.server.activeRuntime(), token.ID, server.ID, "date", "check time", "pending_approval")
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	if _, err := fixture.db.ExecContext(ctx, `UPDATE servers SET host = '127.0.0.2', updated_at = ? WHERE id = ?`, time.Now().UTC().Format(time.RFC3339), server.ID); err != nil {
		t.Fatalf("mutate server: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/approvals/"+strconv.FormatInt(id, 10)+"/run", "", map[string]any{})
	if response.Code != http.StatusConflict {
		t.Fatalf("expected drift conflict, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "server profile or SSH key changed") {
		t.Fatalf("expected server drift reason, got %s", response.Body.String())
	}
	var status string
	var errorText string
	var driftText string
	if err := fixture.db.QueryRowContext(ctx, `SELECT status, error, approval_context_drift FROM command_requests WHERE id = ?`, id).Scan(&status, &errorText, &driftText); err != nil {
		t.Fatalf("read request status: %v", err)
	}
	if status != "stale" || !strings.Contains(errorText, "server profile") || driftText == "" {
		t.Fatalf("unexpected stale request state: status=%q error=%q drift=%q", status, errorText, driftText)
	}
}

func TestApprovalContextDetectsPermissionDrift(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "permission-target")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	runtime := fixture.server.activeRuntime()
	id, err := fixture.server.insertCommandRequest(ctx, runtime, token.ID, server.ID, "uptime", "inspect server", "pending_approval")
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	item, err := fixture.server.getCommandRequest(ctx, runtime, id, 0, "")
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	command, err := fixture.server.commandRequestExecutionCommand(ctx, runtime, id)
	if err != nil {
		t.Fatalf("read execution command: %v", err)
	}
	drifted, reason, err := fixture.server.approvalContextDrift(ctx, runtime, item, command)
	if err != nil {
		t.Fatalf("check drift: %v", err)
	}
	if drifted || reason != "" {
		t.Fatalf("unchanged context should not drift: drifted=%v reason=%q", drifted, reason)
	}

	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleAlwaysRun},
	}}); err != nil {
		t.Fatalf("change permissions: %v", err)
	}
	drifted, reason, err = fixture.server.approvalContextDrift(ctx, runtime, item, command)
	if err != nil {
		t.Fatalf("check changed drift: %v", err)
	}
	if !drifted || !strings.Contains(reason, "token permission changed") {
		t.Fatalf("expected permission drift, got drifted=%v reason=%q", drifted, reason)
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
	var contextHash string
	if err := fixture.db.QueryRowContext(ctx, `SELECT approval_context_hash FROM command_requests WHERE id = ?`, body.RequestID).Scan(&contextHash); err != nil {
		t.Fatalf("read approval context hash: %v", err)
	}
	if contextHash == "" {
		t.Fatalf("approval pending request should store an approval context hash")
	}
	var auditPayload string
	if err := fixture.db.QueryRowContext(ctx, `SELECT payload_json FROM audit_logs WHERE action = 'mcp.exec.approval_pending' ORDER BY id DESC LIMIT 1`).Scan(&auditPayload); err != nil {
		t.Fatalf("read approval audit payload: %v", err)
	}
	if !strings.Contains(auditPayload, contextHash) {
		t.Fatalf("approval audit payload should include context hash %q: %s", contextHash, auditPayload)
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

func TestMCPRestartConsoleSessionClosesSessionAndRunningRequests(t *testing.T) {
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

	now := time.Now().UTC().Format(time.RFC3339)
	sessionResult, err := fixture.db.Exec(`
		INSERT INTO console_sessions (server_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, 'ai session', 'connected', 'stuck output', 120, 32, ?, ?)`,
		server.ID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := sessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read session id: %v", err)
	}
	requestResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, server_id, source, command, reason, status, session_id, created_at)
		VALUES (?, ?, 'mcp', 'sleep 60', 'stuck command', 'running', ?, ?)`,
		token.ID,
		server.ID,
		sessionID,
		now,
	)
	if err != nil {
		t.Fatalf("insert running command request: %v", err)
	}
	requestID, err := requestResult.LastInsertId()
	if err != nil {
		t.Fatalf("read request id: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/console/restart", token.TokenValue, map[string]any{
		"server_id": server.ID,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("restart console failed: %d %s", response.Code, response.Body.String())
	}
	var body mcpRestartConsoleResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode restart response: %v", err)
	}
	if body.Status != "restarted" || body.ServerID != server.ID || body.CanceledRunningRequests != 1 {
		t.Fatalf("unexpected restart response: %#v", body)
	}
	if len(body.ClosedSessionIDs) != 1 || body.ClosedSessionIDs[0] != sessionID {
		t.Fatalf("expected closed session id %d, got %#v", sessionID, body.ClosedSessionIDs)
	}

	var sessionStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM console_sessions WHERE id = ?`, sessionID).Scan(&sessionStatus); err != nil {
		t.Fatalf("read console session status: %v", err)
	}
	if sessionStatus != "closed" {
		t.Fatalf("expected console session closed, got %s", sessionStatus)
	}
	var requestStatus string
	var requestError string
	if err := fixture.db.QueryRow(`SELECT status, error FROM command_requests WHERE id = ?`, requestID).Scan(&requestStatus, &requestError); err != nil {
		t.Fatalf("read command request status: %v", err)
	}
	if requestStatus != "error" || !strings.Contains(requestError, "restarted") {
		t.Fatalf("expected running request error after restart, status=%s error=%q", requestStatus, requestError)
	}

	blockedToken, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "blocked-agent"})
	if err != nil {
		t.Fatalf("create blocked token: %v", err)
	}
	if _, err := fixture.tokens.UpdatePermissions(ctx, blockedToken.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleBlocked},
	}}); err != nil {
		t.Fatalf("update blocked permissions: %v", err)
	}
	blockedResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/console/restart", blockedToken.TokenValue, map[string]any{
		"server_id": server.ID,
	})
	if blockedResponse.Code != http.StatusOK || !strings.Contains(blockedResponse.Body.String(), `"status":"blocked"`) {
		t.Fatalf("blocked token should not restart console, got %d %s", blockedResponse.Code, blockedResponse.Body.String())
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

func TestMCPFileTransferStatusIsTokenScopedAndSanitized(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	allowed := fixture.createKeyAndServer(t, "allowed-transfer")
	blocked := fixture.createKeyAndServer(t, "blocked-transfer")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: allowed.ID, ExecutionRule: tokens.RuleAlwaysRun},
		{ServerID: blocked.ID, ExecutionRule: tokens.RuleBlocked},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	runtime := fixture.server.activeRuntime()
	allowedTransfer, err := runtime.fileTransfers.Create(ctx, filetransfer.CreateRequest{
		ServerID:   allowed.ID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceMCP,
		LocalPath:  "/local/should-not-leak",
		RemotePath: "/var/log/app.log",
		FileName:   "app.log",
		SizeBytes:  20,
		TempPath:   "/tmp/aipermission-secret-download",
	})
	if err != nil {
		t.Fatalf("create allowed transfer: %v", err)
	}
	blockedTransfer, err := runtime.fileTransfers.Create(ctx, filetransfer.CreateRequest{
		ServerID:   blocked.ID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceMCP,
		RemotePath: "/var/log/blocked.log",
		FileName:   "blocked.log",
		TempPath:   "/tmp/blocked-secret",
	})
	if err != nil {
		t.Fatalf("create blocked transfer: %v", err)
	}
	batch, err := runtime.fileTransfers.CreateBatch(ctx, filetransfer.CreateBatchRequest{
		ServerID:    allowed.ID,
		Direction:   filetransfer.DirectionDownload,
		Source:      filetransfer.SourceMCP,
		ArchiveName: "logs.zip",
		Items: []filetransfer.CreateRequest{
			{RemotePath: "/var/log/app.log", FileName: "app.log", SizeBytes: 20, TempPath: "/tmp/aipermission-secret-item"},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if err := runtime.fileTransfers.SetBatchArchive(ctx, batch.ID, "/tmp/aipermission-secret-archive.zip"); err != nil {
		t.Fatalf("set archive path: %v", err)
	}

	listResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/file-transfers?limit=10", token.TokenValue, nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list transfers failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	body := listResponse.Body.String()
	if !strings.Contains(body, `"id":`+strconv.FormatInt(allowedTransfer.ID, 10)) || strings.Contains(body, "/var/log/blocked.log") || strings.Contains(body, "blocked-transfer") {
		t.Fatalf("transfer list should include only allowed transfer: %s", body)
	}
	if strings.Contains(body, "should-not-leak") || strings.Contains(body, "aipermission-secret") || strings.Contains(body, "local_path") || strings.Contains(body, "temp_path") {
		t.Fatalf("transfer list leaked local metadata: %s", body)
	}

	getBlocked := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/file-transfers/"+strconv.FormatInt(blockedTransfer.ID, 10), token.TokenValue, nil)
	if getBlocked.Code != http.StatusNotFound {
		t.Fatalf("blocked transfer detail should look not found, got %d %s", getBlocked.Code, getBlocked.Body.String())
	}

	batchResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/file-transfer-batches/"+strconv.FormatInt(batch.ID, 10), token.TokenValue, nil)
	if batchResponse.Code != http.StatusOK {
		t.Fatalf("get batch failed: %d %s", batchResponse.Code, batchResponse.Body.String())
	}
	batchBody := batchResponse.Body.String()
	if !strings.Contains(batchBody, `"archive_name":"logs.zip"`) || !strings.Contains(batchBody, `"remote_path":"/var/log/app.log"`) {
		t.Fatalf("batch detail should include safe transfer metadata: %s", batchBody)
	}
	if strings.Contains(batchBody, "aipermission-secret") || strings.Contains(batchBody, "archive_path") || strings.Contains(batchBody, "temp_path") || strings.Contains(batchBody, "local_path") {
		t.Fatalf("batch detail leaked local metadata: %s", batchBody)
	}
}

func TestMCPFileTransferManagementSupportsApprovalRequired(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-transfer")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/file-transfers/download-batch", token.TokenValue, map[string]any{
		"server_id":    server.ID,
		"remote_paths": []string{"/var/log/syslog"},
		"archive_name": "logs.zip",
	})
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"status":"pending_approval"`) || !strings.Contains(response.Body.String(), "waiting for local approval") {
		t.Fatalf("approval-required token should create a pending approval transfer, got %d %s", response.Code, response.Body.String())
	}
}

func TestMCPDownloadBatchContentRequiresMCPSource(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-content")
	if _, err := fixture.tokens.UpdatePermissions(ctx, token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleAlwaysRun},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}

	handlers := fileTransferHandlers{fixture.server}
	root, err := handlers.ensureFileTransferTempRoot()
	if err != nil {
		t.Fatalf("create transfer temp root: %v", err)
	}
	payloadPath := filepath.Join(root, "download-payload")
	if err := os.WriteFile(payloadPath, []byte("download payload"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	batch := createCompletedDownloadBatch(t, fixture.server.activeRuntime(), server.ID, filetransfer.SourceMCP, payloadPath)

	response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/file-transfer-batches/"+strconv.FormatInt(batch.ID, 10)+"/download", token.TokenValue, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("expected mcp batch content download to work, got %d %s", response.Code, response.Body.String())
	}
	if response.Body.String() != "download payload" {
		t.Fatalf("unexpected download payload: %q", response.Body.String())
	}

	uiPayloadPath := filepath.Join(root, "ui-download-payload")
	if err := os.WriteFile(uiPayloadPath, []byte("ui payload"), 0o600); err != nil {
		t.Fatalf("write ui payload: %v", err)
	}
	uiBatch := createCompletedDownloadBatch(t, fixture.server.activeRuntime(), server.ID, filetransfer.SourceUI, uiPayloadPath)
	response = performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/file-transfer-batches/"+strconv.FormatInt(uiBatch.ID, 10)+"/download", token.TokenValue, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("mcp should not download ui-created batch content, got %d %s", response.Code, response.Body.String())
	}
}

func createCompletedDownloadBatch(t *testing.T, runtime *databaseRuntime, serverID int64, source string, tempPath string) filetransfer.BatchRecord {
	t.Helper()
	ctx := context.Background()
	batch, err := runtime.fileTransfers.CreateBatch(ctx, filetransfer.CreateBatchRequest{
		ServerID:  serverID,
		Direction: filetransfer.DirectionDownload,
		Source:    source,
		Items: []filetransfer.CreateRequest{{
			RemotePath: "/var/log/app.log",
			FileName:   "app.log",
			SizeBytes:  16,
			TempPath:   tempPath,
		}},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkBatchRunning(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running ok=%v err=%v", ok, err)
	}
	if len(batch.Items) != 1 {
		t.Fatalf("expected one item in batch: %#v", batch)
	}
	if ok, err := runtime.fileTransfers.MarkRunning(ctx, batch.Items[0].ID); err != nil || !ok {
		t.Fatalf("mark transfer running ok=%v err=%v", ok, err)
	}
	if ok, err := runtime.fileTransfers.Complete(ctx, batch.Items[0].ID, 16, "checksum"); err != nil || !ok {
		t.Fatalf("complete transfer ok=%v err=%v", ok, err)
	}
	if err := runtime.fileTransfers.RecalculateBatch(ctx, batch.ID); err != nil {
		t.Fatalf("recalculate batch: %v", err)
	}
	if ok, err := runtime.fileTransfers.CompleteBatch(ctx, batch.ID); err != nil || !ok {
		t.Fatalf("complete batch ok=%v err=%v", ok, err)
	}
	batch, err = runtime.fileTransfers.GetBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("get batch: %v", err)
	}
	return batch
}
