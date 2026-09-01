package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
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

func TestManagementRoutesCoverCredentialsTokensAndConnectorTargets(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	statusResponse := performJSON(handler, http.MethodGet, "/api/status", "", nil)
	if statusResponse.Code != http.StatusOK {
		t.Fatalf("status failed: %d %s", statusResponse.Code, statusResponse.Body.String())
	}
	if strings.Contains(statusResponse.Body.String(), "data_path") || strings.Contains(statusResponse.Body.String(), fixture.server.activeDataPath) {
		t.Fatalf("status should not expose local database paths: %s", statusResponse.Body.String())
	}

	keyResponse := performJSON(handler, http.MethodPost, "/api/connectors/ssh/credentials", "", sshkeys.CreateRequest{Name: "main", KeyType: sshkeys.TypeED25519})
	if keyResponse.Code != http.StatusCreated {
		t.Fatalf("create key failed: %d %s", keyResponse.Code, keyResponse.Body.String())
	}
	key := decodeRouteResponse[sshkeys.SSHKey](t, keyResponse.Body.Bytes())
	privateKey, err := fixture.sshKeys.GetPrivateKey(context.Background(), key.ID)
	if err != nil {
		t.Fatalf("get private key fixture: %v", err)
	}

	importResponse := performJSON(handler, http.MethodPost, "/api/connectors/ssh/credentials/import", "", sshkeys.ImportRequest{Name: "imported", PrivateKey: privateKey.PrivateKey})
	if importResponse.Code != http.StatusCreated {
		t.Fatalf("import key failed: %d %s", importResponse.Code, importResponse.Body.String())
	}
	importedKey := decodeRouteResponse[sshkeys.SSHKey](t, importResponse.Body.Bytes())
	if importedKey.Fingerprint != key.Fingerprint || importedKey.Name != "imported" {
		t.Fatalf("unexpected imported key: %#v", importedKey)
	}

	keyListResponse := performJSON(handler, http.MethodGet, "/api/connectors/ssh/credentials", "", nil)
	if keyListResponse.Code != http.StatusOK || !strings.Contains(keyListResponse.Body.String(), `"name":"main"`) || !strings.Contains(keyListResponse.Body.String(), `"name":"imported"`) {
		t.Fatalf("list keys failed: %d %s", keyListResponse.Code, keyListResponse.Body.String())
	}
	keyGetResponse := performJSON(handler, http.MethodGet, "/api/connectors/ssh/credentials/"+strconv.FormatInt(key.ID, 10), "", nil)
	if keyGetResponse.Code != http.StatusOK {
		t.Fatalf("get key failed: %d %s", keyGetResponse.Code, keyGetResponse.Body.String())
	}
	keyUpdateResponse := performJSON(handler, http.MethodPut, "/api/connectors/ssh/credentials/"+strconv.FormatInt(key.ID, 10), "", sshkeys.UpdateRequest{Name: "main-renamed"})
	if keyUpdateResponse.Code != http.StatusOK || !strings.Contains(keyUpdateResponse.Body.String(), `"name":"main-renamed"`) || !strings.Contains(keyUpdateResponse.Body.String(), "aipermission-main-renamed") {
		t.Fatalf("update key failed: %d %s", keyUpdateResponse.Code, keyUpdateResponse.Body.String())
	}
	sshConfigResponse := performJSON(handler, http.MethodPost, "/api/ssh-config/parse", "", map[string]any{"content": `
Host worker-from-config
  HostName 10.0.0.42
  User ubuntu
  Port 2222
  IdentityFile ~/.ssh/id_ed25519

Host *
  User ignored
`})
	if sshConfigResponse.Code != http.StatusOK || !strings.Contains(sshConfigResponse.Body.String(), "worker-from-config") || strings.Contains(sshConfigResponse.Body.String(), `"ignored"`) {
		t.Fatalf("parse ssh config failed: %d %s", sshConfigResponse.Code, sshConfigResponse.Body.String())
	}

	tokenResponse := performJSON(handler, http.MethodPost, "/api/tokens", "", tokens.CreateRequest{Name: "agent"})
	if tokenResponse.Code != http.StatusCreated {
		t.Fatalf("create token failed: %d %s", tokenResponse.Code, tokenResponse.Body.String())
	}
	assertSensitiveResponseHeaders(t, tokenResponse)
	token := decodeRouteResponse[tokens.CreateResponse](t, tokenResponse.Body.Bytes())
	if token.TokenValue == "" {
		t.Fatalf("create token should return one-time token value")
	}
	if response := performJSON(handler, http.MethodGet, "/api/tokens", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"agent"`) {
		t.Fatalf("list tokens failed: %d %s", response.Code, response.Body.String())
	} else if strings.Contains(response.Body.String(), token.TokenValue) {
		t.Fatalf("list tokens should not expose token value when reusable copy is disabled")
	} else {
		assertSensitiveResponseHeaders(t, response)
	}

	settingsResponse := performJSON(handler, http.MethodPut, "/api/settings/security", "", updateSecuritySettingsRequest{ReusableTokens: true})
	if settingsResponse.Code != http.StatusOK || !strings.Contains(settingsResponse.Body.String(), `"reusable_tokens":true`) {
		t.Fatalf("enable reusable token copy failed: %d %s", settingsResponse.Code, settingsResponse.Body.String())
	}
	reusableTokenResponse := performJSON(handler, http.MethodPost, "/api/tokens", "", tokens.CreateRequest{Name: "reusable-agent"})
	if reusableTokenResponse.Code != http.StatusCreated {
		t.Fatalf("create reusable token failed: %d %s", reusableTokenResponse.Code, reusableTokenResponse.Body.String())
	}
	assertSensitiveResponseHeaders(t, reusableTokenResponse)
	reusableToken := decodeRouteResponse[tokens.CreateResponse](t, reusableTokenResponse.Body.Bytes())
	if response := performJSON(handler, http.MethodGet, "/api/tokens", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), reusableToken.TokenValue) {
		t.Fatalf("list tokens should expose token value when reusable copy is enabled: %d %s", response.Code, response.Body.String())
	} else {
		assertSensitiveResponseHeaders(t, response)
	}

	if response := performJSON(handler, http.MethodPost, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/revoke", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"revoked_at"`) {
		t.Fatalf("revoke token failed: %d %s", response.Code, response.Body.String())
	} else {
		assertSensitiveResponseHeaders(t, response)
	}
	if response := performJSON(handler, http.MethodGet, "/api/audit-logs", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "token.created") || !strings.Contains(response.Body.String(), "token.revoked") {
		t.Fatalf("audit log list should include token lifecycle events: %d %s", response.Code, response.Body.String())
	}

	if response := performJSON(handler, http.MethodDelete, "/api/connectors/ssh/credentials/"+strconv.FormatInt(key.ID, 10), "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete key failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(handler, http.MethodDelete, "/api/connectors/ssh/credentials/"+strconv.FormatInt(importedKey.ID, 10), "", nil); response.Code != http.StatusNoContent {
		t.Fatalf("delete imported key failed: %d %s", response.Code, response.Body.String())
	}
}

func assertSensitiveResponseHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store, private" || response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("sensitive response cache headers = %#v", response.Header())
	}
}

func TestRouteValidationAndLockedMiddleware(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/connector-targets", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked server should reject protected route, got %d", response.Code)
	}
	if response := performJSON(locked.Handler(), http.MethodGet, "/health", "", nil); response.Code != http.StatusOK {
		t.Fatalf("locked server should allow health route, got %d", response.Code)
	}

	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	if response := performJSONWithoutUICookie(handler, http.MethodGet, "/api/connector-targets", "", nil); response.Code != http.StatusUnauthorized {
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
	if response := performJSON(handler, http.MethodGet, "/api/connector-targets/not-a-number", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid id should fail, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodPost, "/api/tokens", "", map[string]any{"name": "x", "extra": true}); response.Code != http.StatusBadRequest {
		t.Fatalf("unknown json field should fail, got %d", response.Code)
	}
}
