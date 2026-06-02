package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/servers"
	"github.com/aipermission/aipermission/backend/internal/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

func decodeRouteResponse[T any](t *testing.T, responseBody []byte) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(responseBody, &value); err != nil {
		t.Fatalf("decode response: %v\n%s", err, string(responseBody))
	}
	return value
}

func TestManagementRoutesCoverSSHKeysServersTokensAndPermissions(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	statusResponse := performJSON(handler, http.MethodGet, "/api/status", "", nil)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status failed: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	if strings.Contains(statusResponse.Body.String(), "data_path") || strings.Contains(statusResponse.Body.String(), fixture.server.activeDataPath) {
		t.Fatalf("status should not expose local database paths: %s", statusResponse.Body.String())
	}

	keyResponse := performJSON(handler, http.MethodPost, "/api/ssh-keys", "", sshkeys.CreateRequest{Name: "main", KeyType: sshkeys.TypeED25519})
	if keyResponse.Code != http.StatusCreated {
		t.Fatalf("create key failed: %d %s", keyResponse.Code, keyResponse.Body.String())
	}
	key := decodeRouteResponse[sshkeys.SSHKey](t, keyResponse.Body.Bytes())

	keyListResponse := performJSON(handler, http.MethodGet, "/api/ssh-keys", "", nil)
	if keyListResponse.Code != http.StatusOK || !strings.Contains(keyListResponse.Body.String(), `"name":"main"`) {
		t.Fatalf("list keys failed: %d %s", keyListResponse.Code, keyListResponse.Body.String())
	}
	keyGetResponse := performJSON(handler, http.MethodGet, "/api/ssh-keys/"+strconv.FormatInt(key.ID, 10), "", nil)
	if keyGetResponse.Code != http.StatusOK {
		t.Fatalf("get key failed: %d %s", keyGetResponse.Code, keyGetResponse.Body.String())
	}

	serverResponse := performJSON(handler, http.MethodPost, "/api/servers", "", servers.CreateRequest{
		Name:     "worker-1",
		Host:     "127.0.0.1",
		Username: "root",
		SSHKeyID: key.ID,
	})
	if serverResponse.Code != http.StatusCreated {
		t.Fatalf("create server failed: %d %s", serverResponse.Code, serverResponse.Body.String())
	}
	server := decodeRouteResponse[servers.Server](t, serverResponse.Body.Bytes())

	if response := performJSON(handler, http.MethodGet, "/api/servers", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"worker-1"`) {
		t.Fatalf("list servers failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/servers/"+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK {
		t.Fatalf("get server failed: %d %s", response.Code, response.Body.String())
	}
	updateResponse := performJSON(handler, http.MethodPut, "/api/servers/"+strconv.FormatInt(server.ID, 10), "", servers.UpdateRequest{
		Name:        "worker-1b",
		Host:        "localhost",
		Port:        2200,
		Username:    "ubuntu",
		SSHKeyID:    key.ID,
		Description: "updated",
	})
	if updateResponse.Code != http.StatusOK || !strings.Contains(updateResponse.Body.String(), `"worker-1b"`) {
		t.Fatalf("update server failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}

	tokenResponse := performJSON(handler, http.MethodPost, "/api/tokens", "", tokens.CreateRequest{Name: "agent"})
	if tokenResponse.Code != http.StatusCreated {
		t.Fatalf("create token failed: %d %s", tokenResponse.Code, tokenResponse.Body.String())
	}
	token := decodeRouteResponse[tokens.CreateResponse](t, tokenResponse.Body.Bytes())
	if token.TokenValue == "" {
		t.Fatalf("create token should return one-time token value")
	}
	if response := performJSON(handler, http.MethodGet, "/api/tokens", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"agent"`) {
		t.Fatalf("list tokens failed: %d %s", response.Code, response.Body.String())
	} else if strings.Contains(response.Body.String(), token.TokenValue) {
		t.Fatalf("list tokens should not expose token value when reusable copy is disabled")
	}

	settingsResponse := performJSON(handler, http.MethodPut, "/api/settings/security", "", updateSecuritySettingsRequest{ReusableTokens: true})
	if settingsResponse.Code != http.StatusOK || !strings.Contains(settingsResponse.Body.String(), `"reusable_tokens":true`) {
		t.Fatalf("enable reusable token copy failed: %d %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	reusableTokenResponse := performJSON(handler, http.MethodPost, "/api/tokens", "", tokens.CreateRequest{Name: "reusable-agent"})
	if reusableTokenResponse.Code != http.StatusCreated {
		t.Fatalf("create reusable token failed: %d %s", reusableTokenResponse.Code, reusableTokenResponse.Body.String())
	}
	reusableToken := decodeRouteResponse[tokens.CreateResponse](t, reusableTokenResponse.Body.Bytes())
	if response := performJSON(handler, http.MethodGet, "/api/tokens", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), reusableToken.TokenValue) {
		t.Fatalf("list tokens should expose token value when reusable copy is enabled: %d %s", response.Code, response.Body.String())
	}

	permissionResponse := performJSON(handler, http.MethodPut, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/permissions", "", tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}})
	if permissionResponse.Code != http.StatusOK || !strings.Contains(permissionResponse.Body.String(), tokens.RuleApprovalRequired) {
		t.Fatalf("update permissions failed: %d %s", permissionResponse.Code, permissionResponse.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/permissions", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"worker-1b"`) {
		t.Fatalf("list permissions failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodPost, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/revoke", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"revoked_at"`) {
		t.Fatalf("revoke token failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/audit-logs", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "token.permissions.updated") || !strings.Contains(response.Body.String(), "token.revoked") {
		t.Fatalf("audit log list should include token lifecycle events: %d %s", response.Code, response.Body.String())
	}

	if response := performJSON(handler, http.MethodDelete, "/api/servers/"+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete server failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodDelete, "/api/ssh-keys/"+strconv.FormatInt(key.ID, 10), "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete key failed: %d %s", response.Code, response.Body.String())
	}
}

func TestRouteValidationAndLockedMiddleware(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/servers", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked server should reject protected route, got %d", response.Code)
	}
	if response := performJSON(locked.Handler(), http.MethodGet, "/health", "", nil); response.Code != http.StatusOK {
		t.Fatalf("locked server should allow health route, got %d", response.Code)
	}

	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	if response := performJSONWithoutUICookie(handler, http.MethodGet, "/api/servers", "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("unlocked management route should require ui session, got %d", response.Code)
	}
	if response := performJSONWithoutUICookie(handler, http.MethodGet, "/api/unlock/status", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"session_required"`) {
		t.Fatalf("unlock status should expose missing ui session state, got %d %s", response.Code, response.Body.String())
	}
	mutatingRequest := httptest.NewRequest(http.MethodPost, "/api/tokens", strings.NewReader(`{"name":"missing-csrf"}`))
	mutatingRequest.Host = "localhost:8080"
	mutatingRequest.RemoteAddr = "127.0.0.1:12345"
	mutatingRequest.Header.Set("Content-Type", "application/json")
	if cookie := currentTestUICookie(); cookie != nil {
		mutatingRequest.AddCookie(cookie)
	}
	mutatingResponse := httptest.NewRecorder()
	handler.ServeHTTP(mutatingResponse, mutatingRequest)
	if mutatingResponse.Code != http.StatusForbidden || !strings.Contains(mutatingResponse.Body.String(), "csrf token required") {
		t.Fatalf("mutating ui route should require csrf token, got %d %s", mutatingResponse.Code, mutatingResponse.Body.String())
	}
	if response := performJSON(handler, http.MethodGet, "/api/servers/not-a-number", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid id should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/tokens", "", map[string]any{"name": "x", "extra": true}); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown json field should fail, got %d", response.Code)
	}
}

func TestLockedLifecycleMutationsRejectCrossSiteAndNonJSONRequests(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	handler := locked.Handler()

	missingOriginBrowser := httptest.NewRequest(http.MethodPost, "/api/unlock/setup", strings.NewReader(`{"database_password":"StrongPassword123","confirm_database_password":"StrongPassword123"}`))
	missingOriginBrowser.Host = "localhost:8080"
	missingOriginBrowser.RemoteAddr = "127.0.0.1:12345"
	missingOriginBrowser.Header.Set("Content-Type", "application/json")
	missingOriginBrowser.Header.Set("User-Agent", "Mozilla/5.0")
	missingOriginBrowserResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingOriginBrowserResponse, missingOriginBrowser)
	if missingOriginBrowserResponse.Code != http.StatusForbidden || !strings.Contains(missingOriginBrowserResponse.Body.String(), "cross-site mutation") {
		t.Fatalf("browser mutation without origin/referer should be rejected, got %d %s", missingOriginBrowserResponse.Code, missingOriginBrowserResponse.Body.String())
	}

	crossSite := httptest.NewRequest(http.MethodPost, "/api/unlock/setup", strings.NewReader(`{"database_password":"StrongPassword123","confirm_database_password":"StrongPassword123"}`))
	crossSite.Host = "localhost:8080"
	crossSite.RemoteAddr = "127.0.0.1:12345"
	crossSite.Header.Set("Content-Type", "application/json")
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSiteResponse := httptest.NewRecorder()
	handler.ServeHTTP(crossSiteResponse, crossSite)
	if crossSiteResponse.Code != http.StatusForbidden || !strings.Contains(crossSiteResponse.Body.String(), "cross-site mutation") {
		t.Fatalf("cross-site locked mutation should be rejected, got %d %s", crossSiteResponse.Code, crossSiteResponse.Body.String())
	}

	wrongContentType := httptest.NewRequest(http.MethodPost, "/api/unlock/setup", strings.NewReader(`{"database_password":"StrongPassword123","confirm_database_password":"StrongPassword123"}`))
	wrongContentType.Host = "localhost:8080"
	wrongContentType.RemoteAddr = "127.0.0.1:12345"
	wrongContentType.Header.Set("Content-Type", "text/plain")
	wrongContentTypeResponse := httptest.NewRecorder()
	handler.ServeHTTP(wrongContentTypeResponse, wrongContentType)
	if wrongContentTypeResponse.Code != http.StatusBadRequest {
		t.Fatalf("non-json lifecycle mutation should fail, got %d %s", wrongContentTypeResponse.Code, wrongContentTypeResponse.Body.String())
	}

	allowedReferer := httptest.NewRequest(http.MethodPost, "/api/unlock/setup", strings.NewReader(`{"password":"StrongPassword123","confirm_password":"StrongPassword123"}`))
	allowedReferer.Host = "localhost:8080"
	allowedReferer.RemoteAddr = "127.0.0.1:12345"
	allowedReferer.Header.Set("Content-Type", "application/json")
	allowedReferer.Header.Set("User-Agent", "Mozilla/5.0")
	allowedReferer.Header.Set("Referer", "http://localhost:3001/")
	allowedReferer.Header.Set("Sec-Fetch-Site", "same-origin")
	allowedRefererResponse := httptest.NewRecorder()
	handler.ServeHTTP(allowedRefererResponse, allowedReferer)
	if allowedRefererResponse.Code == http.StatusForbidden {
		t.Fatalf("same-origin browser mutation with allowed referer should pass boundary, got %d %s", allowedRefererResponse.Code, allowedRefererResponse.Body.String())
	}
}

func fixtureConfigForLockedTest(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Host:           "127.0.0.1",
		Port:           "8080",
		DataPath:       t.TempDir() + "/locked.db",
		GatewaySecret:  "gateway-secret",
		AllowedOrigins: []string{"http://localhost:3001"},
	}
}

func TestApprovalAndMCPRequestRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()
	requestID := insertRouteCommandRequest(t, fixture.db, token.ID, server.ID, "pending_approval")

	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/approvals?status=pending_approval&server_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), pendingApprovalAssistantHint) {
		t.Fatalf("list approvals failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/requests?status=pending_approval", token.TokenValue, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"pending_approval"`) {
		t.Fatalf("mcp list requests failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/requests/"+strconv.FormatInt(requestID, 10), token.TokenValue, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), pendingApprovalAssistantHint) {
		t.Fatalf("mcp get request failed: %d %s", response.Code, response.Body.String())
	}

	declineResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/approvals/"+strconv.FormatInt(requestID, 10)+"/decline", "", declineApprovalRequest{UserNote: "use another path"})
	if declineResponse.Code != http.StatusOK || !strings.Contains(declineResponse.Body.String(), `"declined"`) {
		t.Fatalf("decline approval failed: %d %s", declineResponse.Code, declineResponse.Body.String())
	}
	record, err := fixture.server.getCommandRequest(ctx, runtime, requestID, token.ID)
	if err != nil {
		t.Fatalf("get declined command request: %v", err)
	}
	if record.UserNote == nil || *record.UserNote != "use another path" {
		t.Fatalf("decline note not stored: %#v", record)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/approvals/"+strconv.FormatInt(requestID, 10)+"/run", "", runApprovalRequest{}); response.Code != http.StatusConflict {
		t.Fatalf("running declined request should conflict, got %d", response.Code)
	}
}

func TestHistoryAndAuditPaginationSearchAndDetail(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	now := time.Now().UTC().Format(time.RFC3339)
	dockerResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, server_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'docker ps', 'inspect docker containers', 'completed', 'docker output body', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert docker request: %v", err)
	}
	dockerID, err := dockerResult.LastInsertId()
	if err != nil {
		t.Fatalf("docker request id: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, server_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'uptime', 'inspect uptime', 'completed', 'uptime output body', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert uptime request: %v", err)
	}
	historyResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/approvals?paginated=true&q=docker&limit=1", "", nil)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history search failed: %d %s", historyResponse.Code, historyResponse.Body.String())
	}
	historyPage := decodeRouteResponse[pageResponse[commandRequestRecord]](t, historyResponse.Body.Bytes())
	if historyPage.Total != 1 || len(historyPage.Items) != 1 || historyPage.Items[0].ID != dockerID {
		t.Fatalf("unexpected history page: %#v", historyPage)
	}
	if historyPage.Items[0].Stdout != "" {
		t.Fatalf("history list should not include full stdout: %#v", historyPage.Items[0])
	}
	punctuationSearchResponse := performJSON(fixture.server.Handler(), http.MethodGet, `/api/approvals?paginated=true&q=docker%3A%28%22&limit=1`, "", nil)
	if punctuationSearchResponse.Code != http.StatusOK {
		t.Fatalf("history punctuation search should be sanitized: %d %s", punctuationSearchResponse.Code, punctuationSearchResponse.Body.String())
	}
	historyDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/approvals/"+strconv.FormatInt(dockerID, 10), "", nil)
	if historyDetailResponse.Code != http.StatusOK || !strings.Contains(historyDetailResponse.Body.String(), "docker output body") {
		t.Fatalf("history detail should include output: %d %s", historyDetailResponse.Code, historyDetailResponse.Body.String())
	}

	sensitivePayload := strings.Repeat("x", 700) + " docker image scan"
	fixture.server.writeAudit(ctx, fixture.server.activeRuntime(), "user", &token.ID, server.ID, "docker.audit", map[string]any{
		"detail": sensitivePayload,
	})
	auditResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs?q=image&limit=1", "", nil)
	if auditResponse.Code != http.StatusOK {
		t.Fatalf("audit search failed: %d %s", auditResponse.Code, auditResponse.Body.String())
	}
	auditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, auditResponse.Body.Bytes())
	if auditPage.Total != 1 || len(auditPage.Items) != 1 || auditPage.Items[0].Action != "docker.audit" {
		t.Fatalf("unexpected audit page: %#v", auditPage)
	}
	if len(auditPage.Items[0].PayloadJSON) > 500 {
		t.Fatalf("audit list payload should be a preview, got %d bytes", len(auditPage.Items[0].PayloadJSON))
	}
	auditDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs/"+strconv.FormatInt(auditPage.Items[0].ID, 10), "", nil)
	if auditDetailResponse.Code != http.StatusOK || !strings.Contains(auditDetailResponse.Body.String(), "docker image scan") {
		t.Fatalf("audit detail should include full payload: %d %s", auditDetailResponse.Code, auditDetailResponse.Body.String())
	}
}

func TestRetentionSettingsSaveAndPurgeOldRecords(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, server_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'old command', 'old', 'completed', '', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old command request: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, token_id, server_id, action, payload_json, created_at)
		VALUES ('user', ?, ?, 'old.audit', '{}', ?)`,
		token.ID,
		server.ID,
		old,
	); err != nil {
		t.Fatalf("insert old audit log: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO console_sessions (server_id, name, status, transcript, cols, rows, created_at, updated_at, closed_at)
		VALUES (?, 'old console', 'closed', 'old transcript', 120, 32, ?, ?, ?)`,
		server.ID,
		old,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old console session: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO message_queue (token_id, server_id, direction, message, consumed_at, created_at)
		VALUES (?, ?, 'user_to_ai', 'old message', ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old message: %v", err)
	}

	updateResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/retention", "", updateRetentionSettingsRequest{
		HistoryDays: 7,
		AuditDays:   7,
		ConsoleDays: 7,
		MessageDays: 7,
	})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update retention failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
	if !strings.Contains(updateResponse.Body.String(), `"history_days":7`) {
		t.Fatalf("retention response missing saved value: %s", updateResponse.Body.String())
	}
	assertTableCount(t, fixture.db, "command_requests", 0)
	assertTableCount(t, fixture.db, "console_sessions", 0)
	assertTableCount(t, fixture.db, "message_queue", 0)
	assertTableCount(t, fixture.db, "audit_logs", 1)

	purgeResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/settings/retention/purge", "", purgeRetentionRequest{Target: "audit", Days: 0})
	if purgeResponse.Code != http.StatusBadRequest {
		t.Fatalf("manual purge should reject zero days, got %d %s", purgeResponse.Code, purgeResponse.Body.String())
	}
}

func TestRetentionDisabledKeepsOldRecordsAndManualPurgeDeletes(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, server_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'old command', 'old', 'completed', '', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old command request: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, token_id, server_id, action, payload_json, created_at)
		VALUES ('user', ?, ?, 'old.audit', '{}', ?)`,
		token.ID,
		server.ID,
		old,
	); err != nil {
		t.Fatalf("insert old audit log: %v", err)
	}

	updateResponse := performJSON(fixture.server.Handler(), http.MethodPut, "/api/settings/retention", "", updateRetentionSettingsRequest{})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("disable retention failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
	assertTableCount(t, fixture.db, "command_requests", 1)

	purgeResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/settings/retention/purge", "", purgeRetentionRequest{Target: "history", Days: 7})
	if purgeResponse.Code != http.StatusOK || !strings.Contains(purgeResponse.Body.String(), `"deleted":1`) {
		t.Fatalf("manual history purge failed: %d %s", purgeResponse.Code, purgeResponse.Body.String())
	}
	assertTableCount(t, fixture.db, "command_requests", 0)

	badTargetResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/settings/retention/purge", "", purgeRetentionRequest{Target: "unknown", Days: 7})
	if badTargetResponse.Code != http.StatusBadRequest || !strings.Contains(badTargetResponse.Body.String(), "invalid retention target") {
		t.Fatalf("invalid purge target should fail: %d %s", badTargetResponse.Code, badTargetResponse.Body.String())
	}
}

func assertTableCount(t *testing.T, database *sql.DB, table string, expected int) {
	t.Helper()
	var count int
	if err := database.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != expected {
		t.Fatalf("unexpected %s count: got %d want %d", table, count, expected)
	}
}

func insertRouteCommandRequest(t *testing.T, database *sql.DB, tokenID int64, serverID int64, status string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO command_requests (token_id, server_id, command, reason, status, stdout, stderr, created_at)
		VALUES (?, ?, 'ls', 'test reason', ?, '', '', ?)`,
		tokenID,
		serverID,
		status,
		now,
	)
	if err != nil {
		t.Fatalf("insert command request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("command request id: %v", err)
	}
	return id
}

func TestMessageAndConsoleRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")

	createMessageResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/messages", "", createMessageRequest{TokenID: token.ID, ServerID: &server.ID, Message: "hello agent"})
	if createMessageResponse.Code != http.StatusCreated {
		t.Fatalf("create message failed: %d %s", createMessageResponse.Code, createMessageResponse.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/messages?direction=user_to_ai&server_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello agent") {
		t.Fatalf("list messages failed: %d %s", response.Code, response.Body.String())
	}
	if _, err := fixture.tokens.UpdatePermissions(context.Background(), token.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleAlwaysRun},
	}}); err != nil {
		t.Fatalf("update permissions: %v", err)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/messages", token.TokenValue, createMessageRequest{Message: "from ai", ServerID: &server.ID}); response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "from ai") {
		t.Fatalf("mcp create message failed: %d %s", response.Code, response.Body.String())
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := fixture.db.Exec(`
		INSERT INTO console_sessions (server_id, name, status, transcript, cols, rows, created_at, updated_at, closed_at)
		VALUES (?, 'manual', 'closed', 'hello transcript', 120, 32, ?, ?, ?)`,
		server.ID,
		now,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	sessionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("session id: %v", err)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/sessions?server_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello transcript") {
		t.Fatalf("list console sessions failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "manual") {
		t.Fatalf("get console session failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10)+"/input", "", console.InputRequest{Data: "ls\n"}); response.Code != http.StatusConflict {
		t.Fatalf("input to inactive session should conflict, got %d", response.Code)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10)+"/close", "", nil); response.Code != http.StatusOK {
		t.Fatalf("close console session failed: %d %s", response.Code, response.Body.String())
	}

	blockedServer := fixture.createKeyAndServer(t, "worker-blocked")
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/mcp/messages", token.TokenValue, createMessageRequest{Message: "blocked", ServerID: &blockedServer.ID}); response.Code != http.StatusForbidden {
		t.Fatalf("mcp create message for unauthorized server should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/console?server_id="+strconv.FormatInt(server.ID, 10)+"&tail=20", token.TokenValue, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello transcript") {
		t.Fatalf("mcp read console should return transcript tail, got %d %s", response.Code, response.Body.String())
	}

	approvalToken, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "approval-agent"})
	if err != nil {
		t.Fatalf("create approval token: %v", err)
	}
	if _, err := fixture.tokens.UpdatePermissions(context.Background(), approvalToken.ID, tokens.UpdatePermissionsRequest{Permissions: []tokens.PermissionInput{
		{ServerID: server.ID, ExecutionRule: tokens.RuleApprovalRequired},
	}}); err != nil {
		t.Fatalf("update approval permissions: %v", err)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/console?server_id="+strconv.FormatInt(server.ID, 10)+"&tail=20", approvalToken.TokenValue, nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "requires always_run") || strings.Contains(response.Body.String(), "hello transcript") {
		t.Fatalf("approval_required token should not read shared transcript, got %d %s", response.Code, response.Body.String())
	}
}
