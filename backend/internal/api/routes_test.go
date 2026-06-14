package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/console"
	"github.com/aipermission/aipermission/backend/internal/filetransfer"
	historypkg "github.com/aipermission/aipermission/backend/internal/history"
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

	if response := performJSON(handler, http.MethodPost, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/revoke", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"revoked_at"`) {
		t.Fatalf("revoke token failed: %d %s", response.Code, response.Body.String())
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

func TestConnectorCatalogRoutes(t *testing.T) {
	locked := NewLockedServer(fixtureConfigForLockedTest(t))
	if response := performJSON(locked.Handler(), http.MethodGet, "/api/connectors", "", nil); response.Code != http.StatusLocked {
		t.Fatalf("locked server should reject connector catalog, got %d", response.Code)
	}

	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	listResponse := performJSON(handler, http.MethodGet, "/api/connectors", "", nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list connectors failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	listBody := listResponse.Body.String()
	if !strings.Contains(listBody, `"kind":"postgres"`) || !strings.Contains(listBody, `"kind":"ssh"`) {
		t.Fatalf("connector list missing built-ins: %s", listBody)
	}
	if strings.Index(listBody, `"kind":"postgres"`) > strings.Index(listBody, `"kind":"ssh"`) {
		t.Fatalf("connector list should be stable by kind: %s", listBody)
	}

	detailResponse := performJSON(handler, http.MethodGet, "/api/connectors/postgres", "", nil)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("get postgres connector failed: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	body := detailResponse.Body.String()
	for _, want := range []string{`"kind":"postgres"`, `"target_schema"`, `"credential_schemas"`, `"help"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("postgres connector detail missing %s: %s", want, body)
		}
	}
	if strings.Contains(body, `"actions"`) || strings.Contains(body, `"query_readonly"`) {
		t.Fatalf("connector catalog detail should not expose target/profile-specific actions: %s", body)
	}

	if response := performJSON(handler, http.MethodGet, "/api/connectors/bad-kind", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid connector kind should be bad request, got %d", response.Code)
	}
	if response := performJSON(handler, http.MethodGet, "/api/connectors/redis", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown connector should be not found, got %d", response.Code)
	}
}

func TestUnifiedTargetListIncludesSSHAndConnectorProfiles(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	sshServer := fixture.createKeyAndServer(t, "core-1")
	store := connectortargets.NewStore(fixture.db)
	pgTarget, pgProfile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)

	response := performJSON(handler, http.MethodGet, "/api/targets", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list targets failed: %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{
		`"connector_kind":"ssh"`,
		`"target_name":"core-1"`,
		`"runtime_id":` + strconv.FormatInt(sshServer.ID, 10),
		`"ref":"postgres:` + strconv.FormatInt(pgTarget.ID, 10) + `:` + strconv.FormatInt(pgProfile.ID, 10) + `"`,
		`"profile_label":"readonly"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("target list missing %s: %s", want, body)
		}
	}
}

func TestConnectorTargetRoutesStoreSecretsOnlyInVaultPayload(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	createTarget := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ConnectorKind: "postgres",
		Name:          "main-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "prefer",
		},
	})
	if createTarget.Code != http.StatusCreated {
		t.Fatalf("create connector target failed: %d %s", createTarget.Code, createTarget.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, createTarget.Body.Bytes())
	if target.ID < 1 || target.ConnectorKind != "postgres" || target.Name != "main-db" {
		t.Fatalf("unexpected target response: %#v", target)
	}

	listTargets := performJSON(handler, http.MethodGet, "/api/connector-targets?kind=postgres", "", nil)
	if listTargets.Code != http.StatusOK || !strings.Contains(listTargets.Body.String(), `"main-db"`) {
		t.Fatalf("list connector targets failed: %d %s", listTargets.Code, listTargets.Body.String())
	}

	const password = "secret-password"
	createProfile := performJSON(handler, http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", createConnectorCredentialProfileRequest{
		Kind:  "username_password",
		Label: "readonly",
		Public: map[string]any{
			"username": "app_readonly",
		},
		Secret: map[string]any{
			"password": password,
		},
		RiskLabel: "read-only",
	})
	if createProfile.Code != http.StatusCreated {
		t.Fatalf("create connector profile failed: %d %s", createProfile.Code, createProfile.Body.String())
	}
	if strings.Contains(createProfile.Body.String(), password) {
		t.Fatalf("profile response leaked password: %s", createProfile.Body.String())
	}
	profile := decodeRouteResponse[profileSummary](t, createProfile.Body.Bytes())
	if profile.ID < 1 || profile.Ref != "postgres:"+strconv.FormatInt(target.ID, 10)+":"+strconv.FormatInt(profile.ID, 10) {
		t.Fatalf("unexpected profile response: %#v", profile)
	}
	if profile.Public["username"] != "app_readonly" {
		t.Fatalf("profile public metadata missing: %#v", profile.Public)
	}
	listProfileActions := performJSON(handler, http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles/"+strconv.FormatInt(profile.ID, 10)+"/actions", "", nil)
	if listProfileActions.Code != http.StatusOK || !strings.Contains(listProfileActions.Body.String(), `"query_readonly"`) || !strings.Contains(listProfileActions.Body.String(), `"describe_table"`) {
		t.Fatalf("target/profile action list failed: %d %s", listProfileActions.Code, listProfileActions.Body.String())
	}

	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "connector-agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	permissionExpiresAt := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	updatePermissions := performJSON(handler, http.MethodPut, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/connector-permissions", "", updateConnectorPermissionsRequest{
		Permissions: []connectorPermissionInput{
			{
				TargetID:      target.ID,
				ProfileID:     profile.ID,
				ActionName:    "query_readonly",
				ExecutionRule: string(connectortargets.ActionPermissionApprovalRequired),
				ExpiresAt:     permissionExpiresAt,
			},
		},
	})
	if updatePermissions.Code != http.StatusOK {
		t.Fatalf("update connector permissions failed: %d %s", updatePermissions.Code, updatePermissions.Body.String())
	}
	if !strings.Contains(updatePermissions.Body.String(), `"target_ref":"postgres:`) || !strings.Contains(updatePermissions.Body.String(), `"query_readonly"`) {
		t.Fatalf("connector permission response missing target ref/action: %s", updatePermissions.Body.String())
	}
	listPermissions := performJSON(handler, http.MethodGet, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/connector-permissions", "", nil)
	if listPermissions.Code != http.StatusOK || !strings.Contains(listPermissions.Body.String(), `"profile_label":"readonly"`) {
		t.Fatalf("list connector permissions failed: %d %s", listPermissions.Code, listPermissions.Body.String())
	}
	badPermission := performJSON(handler, http.MethodPut, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/connector-permissions", "", updateConnectorPermissionsRequest{
		Permissions: []connectorPermissionInput{
			{
				TargetID:      target.ID,
				ProfileID:     profile.ID,
				ActionName:    "drop_database",
				ExecutionRule: string(connectortargets.ActionPermissionAlwaysRun),
			},
		},
	})
	if badPermission.Code != http.StatusBadRequest {
		t.Fatalf("unsupported connector action should fail, got %d %s", badPermission.Code, badPermission.Body.String())
	}

	getTarget := performJSON(handler, http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", nil)
	if getTarget.Code != http.StatusOK || strings.Contains(getTarget.Body.String(), password) || !strings.Contains(getTarget.Body.String(), `"profiles"`) {
		t.Fatalf("get connector target failed or leaked secret: %d %s", getTarget.Code, getTarget.Body.String())
	}
	listProfiles := performJSON(handler, http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", nil)
	if listProfiles.Code != http.StatusOK || strings.Contains(listProfiles.Body.String(), password) || !strings.Contains(listProfiles.Body.String(), `"readonly"`) {
		t.Fatalf("list connector profiles failed or leaked secret: %d %s", listProfiles.Code, listProfiles.Body.String())
	}

	var encryptedSecret string
	if err := fixture.db.QueryRow(`SELECT encrypted_secret_json FROM connector_credential_profiles WHERE id = ?`, profile.ID).Scan(&encryptedSecret); err != nil {
		t.Fatalf("read encrypted profile secret: %v", err)
	}
	if encryptedSecret == "" || strings.Contains(encryptedSecret, password) {
		t.Fatalf("secret was not encrypted: %q", encryptedSecret)
	}

	updateTarget := performJSON(handler, http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", updateConnectorTargetRequest{
		Name: "main-db-renamed",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5433,
			"database":        "app2",
			"ssl_mode":        "require",
		},
	})
	if updateTarget.Code != http.StatusOK || !strings.Contains(updateTarget.Body.String(), `"main-db-renamed"`) {
		t.Fatalf("update connector target failed: %d %s", updateTarget.Code, updateTarget.Body.String())
	}
	updateProfile := performJSON(handler, http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles/"+strconv.FormatInt(profile.ID, 10), "", updateConnectorCredentialProfileRequest{
		Kind:  "username_password",
		Label: "readonly-renamed",
		Public: map[string]any{
			"username": "app_reader",
		},
		RiskLabel: "read-only",
	})
	if updateProfile.Code != http.StatusOK || strings.Contains(updateProfile.Body.String(), password) || !strings.Contains(updateProfile.Body.String(), `"readonly-renamed"`) {
		t.Fatalf("update connector profile failed or leaked secret: %d %s", updateProfile.Code, updateProfile.Body.String())
	}
	var encryptedAfterUpdate string
	if err := fixture.db.QueryRow(`SELECT encrypted_secret_json FROM connector_credential_profiles WHERE id = ?`, profile.ID).Scan(&encryptedAfterUpdate); err != nil {
		t.Fatalf("read encrypted profile secret after update: %v", err)
	}
	if encryptedAfterUpdate != encryptedSecret {
		t.Fatalf("profile update without secret should preserve encrypted secret")
	}

	var auditPayloads string
	if err := fixture.db.QueryRow(`SELECT COALESCE(group_concat(payload_json, char(10)), '') FROM audit_logs WHERE action LIKE 'connector.%'`).Scan(&auditPayloads); err != nil {
		t.Fatalf("read connector audit payloads: %v", err)
	}
	if strings.Contains(auditPayloads, password) {
		t.Fatalf("audit payload leaked password: %s", auditPayloads)
	}

	unsupportedTarget := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ConnectorKind: "redis",
		Name:          "cache",
	})
	if unsupportedTarget.Code != http.StatusBadRequest {
		t.Fatalf("unsupported connector kind should fail, got %d %s", unsupportedTarget.Code, unsupportedTarget.Body.String())
	}
	unsupportedProfile := performJSON(handler, http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", createConnectorCredentialProfileRequest{
		Kind:  "api_key",
		Label: "bad",
	})
	if unsupportedProfile.Code != http.StatusBadRequest {
		t.Fatalf("unsupported profile kind should fail, got %d %s", unsupportedProfile.Code, unsupportedProfile.Body.String())
	}
	invalidTargetSchema := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ConnectorKind: "postgres",
		Name:          "bad-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"database":        "app",
			"unexpected":      "nope",
		},
	})
	if invalidTargetSchema.Code != http.StatusBadRequest {
		t.Fatalf("unknown target schema field should fail, got %d %s", invalidTargetSchema.Code, invalidTargetSchema.Body.String())
	}

	deleteTarget := performJSON(handler, http.MethodDelete, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", nil)
	if deleteTarget.Code != http.StatusOK {
		t.Fatalf("delete connector target failed: %d %s", deleteTarget.Code, deleteTarget.Body.String())
	}
	getDeletedTarget := performJSON(handler, http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", nil)
	if getDeletedTarget.Code != http.StatusNotFound {
		t.Fatalf("deleted connector target should be gone, got %d %s", getDeletedTarget.Code, getDeletedTarget.Body.String())
	}
}

func TestConnectorTargetWithProfileRoutesAreAtomic(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()

	createWithProfile := performJSON(handler, http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
		Target: createConnectorTargetRequest{
			ConnectorKind: "postgres",
			Name:          "atomic-db",
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            "127.0.0.1",
				"port":            5432,
				"database":        "app",
				"ssl_mode":        "prefer",
			},
		},
		Profile: createConnectorCredentialProfileRequest{
			Kind:  "username_password",
			Label: "readonly",
			Public: map[string]any{
				"username": "app_readonly",
			},
			Secret: map[string]any{
				"password": "secret-password",
			},
			RiskLabel: "read-only",
		},
	})
	if createWithProfile.Code != http.StatusCreated {
		t.Fatalf("create target with profile failed: %d %s", createWithProfile.Code, createWithProfile.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, createWithProfile.Body.Bytes())
	if target.ID < 1 || len(target.Profiles) != 1 || target.Profiles[0].Label != "readonly" {
		t.Fatalf("unexpected atomic create response: %#v", target)
	}

	failedCreate := performJSON(handler, http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
		Target: createConnectorTargetRequest{
			ConnectorKind: "postgres",
			Name:          "should-rollback",
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            "127.0.0.1",
				"port":            5432,
				"database":        "app",
				"ssl_mode":        "prefer",
			},
		},
		Profile: createConnectorCredentialProfileRequest{
			Kind:  "unsupported",
			Label: "bad",
		},
	})
	if failedCreate.Code != http.StatusBadRequest {
		t.Fatalf("invalid profile should reject atomic create, got %d %s", failedCreate.Code, failedCreate.Body.String())
	}
	var rolledBackTargets int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_targets WHERE name = 'should-rollback'`).Scan(&rolledBackTargets); err != nil {
		t.Fatalf("count rolled back targets: %v", err)
	}
	if rolledBackTargets != 0 {
		t.Fatalf("atomic create left a target without a profile")
	}

	failedUpdate := performJSON(handler, http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/with-profile/999999", "", updateConnectorTargetWithProfileRequest{
		Target: updateConnectorTargetRequest{
			Name: "should-not-stick",
			Config: map[string]any{
				"connection_mode": "direct",
				"host":            "127.0.0.1",
				"port":            5433,
				"database":        "app2",
				"ssl_mode":        "require",
			},
		},
		Profile: updateConnectorCredentialProfileRequest{
			Kind:  "username_password",
			Label: "missing-profile",
			Public: map[string]any{
				"username": "app_reader",
			},
		},
	})
	if failedUpdate.Code != http.StatusNotFound {
		t.Fatalf("missing profile should reject atomic update, got %d %s", failedUpdate.Code, failedUpdate.Body.String())
	}
	var targetName string
	var targetConfig string
	if err := fixture.db.QueryRow(`SELECT name, config_json FROM connector_targets WHERE id = ?`, target.ID).Scan(&targetName, &targetConfig); err != nil {
		t.Fatalf("read target after failed update: %v", err)
	}
	if targetName != "atomic-db" || !strings.Contains(targetConfig, `"database":"app"`) || strings.Contains(targetConfig, "app2") {
		t.Fatalf("failed atomic update changed target: name=%q config=%s", targetName, targetConfig)
	}
}

func TestSSHConnectorProfileRoutesCanonicalizeKeyMetadata(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	key, err := fixture.sshKeys.Create(context.Background(), sshkeys.CreateRequest{Name: "main", KeyType: sshkeys.TypeED25519})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}

	createTarget := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ConnectorKind: "ssh",
		Name:          "worker-1",
		Config: map[string]any{
			"host": "127.0.0.1",
			"port": 22,
		},
	})
	if createTarget.Code != http.StatusCreated {
		t.Fatalf("create ssh target failed: %d %s", createTarget.Code, createTarget.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, createTarget.Body.Bytes())

	badProfile := performJSON(handler, http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", createConnectorCredentialProfileRequest{
		Kind:  "private_key",
		Label: "root",
		Public: map[string]any{
			"username":   "root",
			"ssh_key_id": 999999,
		},
	})
	if badProfile.Code != http.StatusBadRequest {
		t.Fatalf("dangling ssh_key_id should fail, got %d %s", badProfile.Code, badProfile.Body.String())
	}

	createProfile := performJSON(handler, http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", createConnectorCredentialProfileRequest{
		Kind:  "private_key",
		Label: "root",
		Public: map[string]any{
			"username":    "root",
			"ssh_key_id":  key.ID,
			"key_name":    "caller-forged-name",
			"key_type":    "caller-forged-type",
			"fingerprint": "caller-forged-fingerprint",
		},
	})
	if createProfile.Code != http.StatusCreated {
		t.Fatalf("create ssh profile failed: %d %s", createProfile.Code, createProfile.Body.String())
	}
	profile := decodeRouteResponse[profileSummary](t, createProfile.Body.Bytes())
	if profile.Public["key_name"] != key.Name || profile.Public["key_type"] != key.KeyType || profile.Public["fingerprint"] != key.Fingerprint {
		t.Fatalf("ssh profile public metadata was not canonicalized: %#v key=%#v", profile.Public, key)
	}
}

func TestConnectorProfileRoutesAllowGenericSSHProfileCreate(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	key := decodeRouteResponse[sshkeys.SSHKey](t, performJSON(handler, http.MethodPost, "/api/connectors/ssh/credentials", "", sshkeys.CreateRequest{Name: "main", KeyType: sshkeys.TypeED25519}).Body.Bytes())
	createTarget := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ConnectorKind: "ssh",
		Name:          "core-ssh",
		Config: map[string]any{
			"host": "127.0.0.1",
			"port": 22,
		},
	})
	if createTarget.Code != http.StatusCreated {
		t.Fatalf("create ssh target failed: %d %s", createTarget.Code, createTarget.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, createTarget.Body.Bytes())
	response := performJSON(handler, http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10)+"/profiles", "", createConnectorCredentialProfileRequest{
		Kind:  "private_key",
		Label: "extra",
		Public: map[string]any{
			"username":   "root",
			"ssh_key_id": key.ID,
		},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("generic SSH profile create should succeed, got %d %s", response.Code, response.Body.String())
	}
	profile := decodeRouteResponse[profileSummary](t, response.Body.Bytes())
	if profile.ConnectorKind != "ssh" || profile.Kind != "private_key" || profile.Public["username"] != "root" {
		t.Fatalf("unexpected ssh profile summary: %#v", profile)
	}

	getTarget := performJSON(handler, http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", nil)
	if getTarget.Code != http.StatusOK {
		t.Fatalf("get ssh connector target failed: %d %s", getTarget.Code, getTarget.Body.String())
	}
	roundTripTarget := decodeRouteResponse[connectorTargetResponse](t, getTarget.Body.Bytes())
	if _, ok := roundTripTarget.Config["username"]; ok {
		t.Fatalf("ssh target config should not expose username: %#v", roundTripTarget.Config)
	}
	if _, ok := roundTripTarget.Config["ssh_key_id"]; ok {
		t.Fatalf("ssh target config should not expose ssh_key_id: %#v", roundTripTarget.Config)
	}
	if len(roundTripTarget.Profiles) != 1 || roundTripTarget.Profiles[0].Public["username"] != "root" {
		t.Fatalf("ssh profile metadata missing after target GET: %#v", roundTripTarget.Profiles)
	}
	updateTarget := performJSON(handler, http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", updateConnectorTargetRequest{
		Name:   roundTripTarget.Name,
		Config: roundTripTarget.Config,
	})
	if updateTarget.Code != http.StatusOK {
		t.Fatalf("ssh target GET -> PUT round-trip failed: %d %s", updateTarget.Code, updateTarget.Body.String())
	}

	invalidTarget := performJSON(handler, http.MethodPost, "/api/connector-targets", "", createConnectorTargetRequest{
		ConnectorKind: "ssh",
		Name:          "bad-ssh",
		Config: map[string]any{
			"host":       "127.0.0.1",
			"port":       22,
			"username":   "root",
			"ssh_key_id": key.ID,
		},
	})
	if invalidTarget.Code != http.StatusBadRequest {
		t.Fatalf("ssh target create should reject profile fields in target config, got %d %s", invalidTarget.Code, invalidTarget.Body.String())
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

func TestConsoleCommandRequestDetail(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()
	requestID := insertRouteCommandRequest(t, fixture.db, token.ID, server.ID, "running")
	manualID := insertManualRouteCommandRequest(t, fixture.db, server.ID, "nano /etc/hosts ...", "interactive_editor")
	detailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/command-requests/"+strconv.FormatInt(manualID, 10), "", nil)
	if detailResponse.Code != http.StatusOK || !strings.Contains(detailResponse.Body.String(), `"source":"manual"`) || !strings.Contains(detailResponse.Body.String(), `"tracking_reason":"interactive_editor"`) {
		t.Fatalf("manual command request detail failed: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	if manualID < 1 {
		t.Fatalf("expected manual request id")
	}
	record, err := fixture.server.getCommandRequest(ctx, runtime, requestID, token.ID, commandRequestSourceMCP)
	if err != nil {
		t.Fatalf("get command request: %v", err)
	}
	if record.Status != "running" || record.AssistantHint != runningCommandRequestAssistantHint {
		t.Fatalf("running command request should keep polling hint: %#v", record)
	}
}

func TestBulkConsoleCommandCreatesManualHistoryRows(t *testing.T) {
	fixture := newAPITestFixture(t)
	serverOne := fixture.createKeyAndServer(t, "bulk-one")
	serverTwo := fixture.createKeyAndServer(t, "bulk-two")
	if _, err := fixture.db.Exec(`
		UPDATE connector_targets
		SET config_json = '{"host":"127.0.0.1","port":1,"description":"closed port"}'
		WHERE id IN (?, ?)`,
		serverOne.TargetID,
		serverTwo.TargetID,
	); err != nil {
		t.Fatalf("move test ssh targets to closed port: %v", err)
	}

	missingConfirmation := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs: []int64{serverOne.ID, serverTwo.ID},
		Command:   "hostname",
		Reason:    "bulk smoke",
	})
	if missingConfirmation.Code != http.StatusBadRequest || !strings.Contains(missingConfirmation.Body.String(), "RUN ON 2 TARGETS") {
		t.Fatalf("bulk command should require exact confirmation, got %d %s", missingConfirmation.Code, missingConfirmation.Body.String())
	}

	duplicateServer := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs:    []int64{serverOne.ID, serverOne.ID},
		Command:      "hostname",
		Reason:       "bulk smoke",
		Confirmation: "RUN ON 2 TARGETS",
	})
	if duplicateServer.Code != http.StatusBadRequest || !strings.Contains(duplicateServer.Body.String(), "duplicates") {
		t.Fatalf("bulk command should reject duplicate targets, got %d %s", duplicateServer.Code, duplicateServer.Body.String())
	}

	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/bulk-exec", "", bulkConsoleCommandRequest{
		TargetIDs:    []int64{serverOne.ID, serverTwo.ID},
		Command:      "hostname",
		Reason:       "bulk smoke",
		Confirmation: "RUN ON 2 TARGETS",
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("bulk command failed: %d %s", response.Code, response.Body.String())
	}
	result := decodeRouteResponse[bulkConsoleCommandResponse](t, response.Body.Bytes())
	if result.Parallelism != bulkConsoleCommandParallelism || len(result.Items) != 2 {
		t.Fatalf("unexpected bulk command response: %#v", result)
	}

	var rows int
	if err := fixture.db.QueryRow(`
		SELECT COUNT(*)
		FROM command_requests
		WHERE source = 'manual'
			AND token_id IS NULL
			AND encrypted_command <> ''
			AND command = 'hostname'
			AND reason = 'bulk smoke'
			AND status IN ('running', 'error')`,
	).Scan(&rows); err != nil {
		t.Fatalf("count bulk command requests: %v", err)
	}
	if rows != 2 {
		t.Fatalf("expected two manual bulk command rows, got %d", rows)
	}

	var auditRows int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM audit_logs WHERE action = 'console.bulk_exec.started'`).Scan(&auditRows); err != nil {
		t.Fatalf("count bulk command audit: %v", err)
	}
	if auditRows != 1 {
		t.Fatalf("expected one bulk audit event, got %d", auditRows)
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
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
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
	if err := historypkg.NewStore(fixture.db).SyncCommandRequest(ctx, dockerID); err != nil {
		t.Fatalf("sync docker request to history: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'uptime', 'inspect uptime', 'completed', 'uptime output body', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert uptime request: %v", err)
	}
	historyResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?q=docker&limit=1", "", nil)
	if historyResponse.Code != http.StatusOK {
		t.Fatalf("history search failed: %d %s", historyResponse.Code, historyResponse.Body.String())
	}
	historyPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, historyResponse.Body.Bytes())
	if historyPage.Total != 1 || len(historyPage.Items) != 1 || historyPage.Items[0].SourceRefID != dockerID {
		t.Fatalf("unexpected history page: %#v", historyPage)
	}
	if historyPage.Items[0].OutputText != "" {
		t.Fatalf("history list should not include full output: %#v", historyPage.Items[0])
	}
	punctuationSearchResponse := performJSON(fixture.server.Handler(), http.MethodGet, `/api/history?q=docker%3A%28%22&limit=1`, "", nil)
	if punctuationSearchResponse.Code != http.StatusOK {
		t.Fatalf("history punctuation search should be sanitized: %d %s", punctuationSearchResponse.Code, punctuationSearchResponse.Body.String())
	}
	historyDetailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/command-requests/"+strconv.FormatInt(dockerID, 10), "", nil)
	if historyDetailResponse.Code != http.StatusOK || !strings.Contains(historyDetailResponse.Body.String(), "docker output body") {
		t.Fatalf("history detail should include output: %d %s", historyDetailResponse.Code, historyDetailResponse.Body.String())
	}
	unifiedDockerResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?q=docker&limit=1", "", nil)
	if unifiedDockerResponse.Code != http.StatusOK {
		t.Fatalf("unified history search failed: %d %s", unifiedDockerResponse.Code, unifiedDockerResponse.Body.String())
	}
	unifiedDockerPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, unifiedDockerResponse.Body.Bytes())
	if unifiedDockerPage.Total != 1 || len(unifiedDockerPage.Items) != 1 || unifiedDockerPage.Items[0].SourceRefID != dockerID {
		t.Fatalf("unexpected unified docker history page: %#v", unifiedDockerPage)
	}
	dockerHistoryID := unifiedDockerPage.Items[0].ID
	store := connectortargets.NewStore(fixture.db)
	sshConnectorRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             server.TargetID,
		ProfileID:            server.ProfileID,
		ConnectorKind:        "ssh",
		ActionName:           "exec",
		Input:                map[string]any{"command": "whoami"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert ssh connector action request: %v", err)
	}
	if _, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:          sshConnectorRequest.ID,
		Status:      connectors.ResultCompleted,
		Output:      map[string]any{"stdout": "root\n"},
		DisplayText: "root\n",
	}); err != nil {
		t.Fatalf("finish ssh connector action request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(ctx, sshConnectorRequest.ID); err != nil {
		t.Fatalf("sync ssh connector action to history: %v", err)
	}
	sshTargetHistoryResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=ssh&target_id="+strconv.FormatInt(server.TargetID, 10)+"&limit=10", "", nil)
	if sshTargetHistoryResponse.Code != http.StatusOK {
		t.Fatalf("ssh target unified history filter failed: %d %s", sshTargetHistoryResponse.Code, sshTargetHistoryResponse.Body.String())
	}
	sshTargetHistoryPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, sshTargetHistoryResponse.Body.Bytes())
	if sshTargetHistoryPage.Total != 2 {
		t.Fatalf("ssh target filter should include live-console command and connector action rows, got %#v", sshTargetHistoryPage)
	}
	sshRuntimeHistoryResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=ssh&runtime_id="+strconv.FormatInt(server.ID, 10)+"&limit=10", "", nil)
	if sshRuntimeHistoryResponse.Code != http.StatusOK {
		t.Fatalf("ssh runtime unified history filter failed: %d %s", sshRuntimeHistoryResponse.Code, sshRuntimeHistoryResponse.Body.String())
	}
	sshRuntimeHistoryPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, sshRuntimeHistoryResponse.Body.Bytes())
	if sshRuntimeHistoryPage.Total != 1 || len(sshRuntimeHistoryPage.Items) != 1 || sshRuntimeHistoryPage.Items[0].SourceRefID != dockerID {
		t.Fatalf("ssh runtime filter should isolate the live-console command row, got %#v", sshRuntimeHistoryPage)
	}
	pgTarget, pgProfile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	connectorRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             pgTarget.ID,
		ProfileID:            pgProfile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "query_readonly",
		Input:                map[string]any{"sql": "select customer from invoices where customer = 'needle_customer'"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert connector request: %v", err)
	}
	if _, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:     connectorRequest.ID,
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"rows": []any{map[string]any{"customer": "needle_customer"}},
		},
	}); err != nil {
		t.Fatalf("finish connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(ctx, connectorRequest.ID); err != nil {
		t.Fatalf("sync connector request to history: %v", err)
	}
	connectorJSONSearchResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?q=needle_customer&connector_kind=postgres&limit=1", "", nil)
	if connectorJSONSearchResponse.Code != http.StatusOK {
		t.Fatalf("unified connector json search failed: %d %s", connectorJSONSearchResponse.Code, connectorJSONSearchResponse.Body.String())
	}
	connectorJSONSearchPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, connectorJSONSearchResponse.Body.Bytes())
	if connectorJSONSearchPage.Total != 1 || len(connectorJSONSearchPage.Items) != 1 || connectorJSONSearchPage.Items[0].SourceRefID != connectorRequest.ID {
		t.Fatalf("unexpected unified connector json search page: %#v", connectorJSONSearchPage)
	}
	encryptedOtherSecret, err := fixture.server.activeRuntime().vault.EncryptJSON(map[string]any{"password": "other-secret"})
	if err != nil {
		t.Fatalf("encrypt second profile secret: %v", err)
	}
	secondPGProfile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            pgTarget.ID,
		ConnectorKind:       "postgres",
		Kind:                "username_password",
		Label:               "analytics",
		Public:              map[string]any{"username": "analytics_readonly"},
		EncryptedSecretJSON: encryptedOtherSecret,
	})
	if err != nil {
		t.Fatalf("create second postgres profile: %v", err)
	}
	secondConnectorRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             pgTarget.ID,
		ProfileID:            secondPGProfile.ID,
		ConnectorKind:        "postgres",
		ActionName:           "get_tables",
		Input:                map[string]any{"schema": "public"},
		EncryptedPayloadJSON: "encrypted-payload-2",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert second connector request: %v", err)
	}
	if _, err := store.FinishActionRequest(ctx, connectortargets.FinishActionRequestInput{
		ID:     secondConnectorRequest.ID,
		Status: connectors.ResultCompleted,
		Output: map[string]any{
			"tables": []any{"analytics_events"},
		},
	}); err != nil {
		t.Fatalf("finish second connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(ctx, secondConnectorRequest.ID); err != nil {
		t.Fatalf("sync second connector request to history: %v", err)
	}
	profileFilterResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?connector_kind=postgres&target_id="+strconv.FormatInt(pgTarget.ID, 10)+"&profile_id="+strconv.FormatInt(pgProfile.ID, 10)+"&limit=10", "", nil)
	if profileFilterResponse.Code != http.StatusOK {
		t.Fatalf("postgres profile history filter failed: %d %s", profileFilterResponse.Code, profileFilterResponse.Body.String())
	}
	profileFilterPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, profileFilterResponse.Body.Bytes())
	if profileFilterPage.Total != 1 || len(profileFilterPage.Items) != 1 || profileFilterPage.Items[0].SourceRefID != connectorRequest.ID {
		t.Fatalf("postgres profile filter should isolate one profile, got %#v", profileFilterPage)
	}
	labelResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history-labels", "", createHistoryLabelRequest{Name: "issue-440"})
	if labelResponse.Code != http.StatusCreated {
		t.Fatalf("create history label failed: %d %s", labelResponse.Code, labelResponse.Body.String())
	}
	label := decodeRouteResponse[historyLabelRecord](t, labelResponse.Body.Bytes())
	reusedLabelResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history-labels", "", createHistoryLabelRequest{Name: "issue-440"})
	if reusedLabelResponse.Code != http.StatusOK {
		t.Fatalf("reused history label should return ok, got %d %s", reusedLabelResponse.Code, reusedLabelResponse.Body.String())
	}
	attachResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels", "", attachHistoryLabelRequest{Name: "docker"})
	if attachResponse.Code != http.StatusCreated || !strings.Contains(attachResponse.Body.String(), `"docker"`) {
		t.Fatalf("attach history label by name failed: %d %s", attachResponse.Code, attachResponse.Body.String())
	}
	attachExistingResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels", "", attachHistoryLabelRequest{LabelID: label.ID})
	if attachExistingResponse.Code != http.StatusOK || !strings.Contains(attachExistingResponse.Body.String(), `"issue-440"`) {
		t.Fatalf("attach existing history label failed: %d %s", attachExistingResponse.Code, attachExistingResponse.Body.String())
	}
	labelListResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history-labels", "", nil)
	if labelListResponse.Code != http.StatusOK || !strings.Contains(labelListResponse.Body.String(), `"issue-440"`) || !strings.Contains(labelListResponse.Body.String(), `"docker"`) {
		t.Fatalf("list history labels failed: %d %s", labelListResponse.Code, labelListResponse.Body.String())
	}
	unifiedLabelResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?label_id="+strconv.FormatInt(label.ID, 10), "", nil)
	if unifiedLabelResponse.Code != http.StatusOK {
		t.Fatalf("unified history label filter failed: %d %s", unifiedLabelResponse.Code, unifiedLabelResponse.Body.String())
	}
	unifiedLabelPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, unifiedLabelResponse.Body.Bytes())
	if unifiedLabelPage.Total != 1 || len(unifiedLabelPage.Items) != 1 || unifiedLabelPage.Items[0].SourceRefID != dockerID || len(unifiedLabelPage.Items[0].Labels) != 2 {
		t.Fatalf("unexpected unified history label page: %#v", unifiedLabelPage)
	}
	detachResponse := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels/"+strconv.FormatInt(label.ID, 10), "", nil)
	if detachResponse.Code != http.StatusOK || strings.Contains(detachResponse.Body.String(), `"issue-440"`) {
		t.Fatalf("detach history label failed: %d %s", detachResponse.Code, detachResponse.Body.String())
	}
	unifiedDetachedResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?label_id="+strconv.FormatInt(label.ID, 10), "", nil)
	unifiedDetachedPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, unifiedDetachedResponse.Body.Bytes())
	if unifiedDetachedResponse.Code != http.StatusOK || unifiedDetachedPage.Total != 0 {
		t.Fatalf("detached label should filter as empty in unified history: %d %#v", unifiedDetachedResponse.Code, unifiedDetachedPage)
	}
	missingDetachResponse := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/history/"+strconv.FormatInt(dockerHistoryID, 10)+"/labels/"+strconv.FormatInt(label.ID, 10), "", nil)
	if missingDetachResponse.Code != http.StatusNotFound {
		t.Fatalf("missing label relationship should return not found, got %d %s", missingDetachResponse.Code, missingDetachResponse.Body.String())
	}
	deleteLabelResponse := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/history-labels/"+strconv.FormatInt(label.ID, 10), "", nil)
	if deleteLabelResponse.Code != http.StatusOK {
		t.Fatalf("delete history label failed: %d %s", deleteLabelResponse.Code, deleteLabelResponse.Body.String())
	}
	filterDeletedLabelResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/history?label_id="+strconv.FormatInt(label.ID, 10), "", nil)
	filterDeletedLabelPage := decodeRouteResponse[pageResponse[historyEntryRecord]](t, filterDeletedLabelResponse.Body.Bytes())
	if filterDeletedLabelResponse.Code != http.StatusOK || filterDeletedLabelPage.Total != 0 {
		t.Fatalf("deleted label should filter as empty: %d %#v", filterDeletedLabelResponse.Code, filterDeletedLabelPage)
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
	fixture.server.writeAudit(ctx, fixture.server.activeRuntime(), "mcp", &token.ID, 0, "connector_action.completed", map[string]any{
		"connector_kind":    "ssh",
		"target_id":         server.TargetID,
		"profile_id":        server.ProfileID,
		"action_request_id": int64(777),
		"detail":            "connector audit metadata",
	})
	connectorAuditResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/audit-logs?connector_kind=ssh&target_id="+strconv.FormatInt(server.TargetID, 10), "", nil)
	if connectorAuditResponse.Code != http.StatusOK {
		t.Fatalf("connector audit filter failed: %d %s", connectorAuditResponse.Code, connectorAuditResponse.Body.String())
	}
	connectorAuditPage := decodeRouteResponse[pageResponse[auditLogRecord]](t, connectorAuditResponse.Body.Bytes())
	if connectorAuditPage.Total != 1 || len(connectorAuditPage.Items) != 1 {
		t.Fatalf("unexpected connector audit page: %#v", connectorAuditPage)
	}
	item := connectorAuditPage.Items[0]
	if item.ConnectorKind != "ssh" || item.TargetID == nil || *item.TargetID != server.TargetID || item.TargetName != "worker-1" || item.ActionRequestID == nil || *item.ActionRequestID != 777 {
		t.Fatalf("connector audit metadata missing: %#v", item)
	}
}

func TestConnectorTargetDeleteFinalizesSSHRuntimeState(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	token, err := fixture.tokens.Create(ctx, tokens.CreateRequest{Name: "agent"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	server := fixture.createKeyAndServer(t, "delete-me")
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, 'delete session', 'connected', '', 120, 32, ?, ?)`,
		server.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert console session: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, source, command, reason, status, created_at)
		VALUES (?, ?, 'mcp', 'sleep 100', 'delete target cleanup', 'running', ?)`,
		token.ID,
		server.ID,
		now,
	); err != nil {
		t.Fatalf("insert running command request: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	actionRequest, err := store.InsertActionRequest(ctx, connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             server.TargetID,
		ProfileID:            server.ProfileID,
		ConnectorKind:        "ssh",
		ActionName:           "exec",
		Input:                map[string]any{"command": "sleep 100"},
		EncryptedPayloadJSON: "encrypted-payload",
		Status:               connectors.ResultRunning,
	})
	if err != nil {
		t.Fatalf("insert connector action request: %v", err)
	}

	response := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/connector-targets/"+strconv.FormatInt(server.TargetID, 10), "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("delete connector target failed: %d %s", response.Code, response.Body.String())
	}
	var commandStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM command_requests WHERE runtime_id = ?`, server.ID).Scan(&commandStatus); err != nil {
		t.Fatalf("read command status: %v", err)
	}
	if commandStatus != "error" {
		t.Fatalf("running command should be marked error after target delete, got %q", commandStatus)
	}
	var sessionStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM console_sessions WHERE runtime_id = ?`, server.ID).Scan(&sessionStatus); err != nil {
		t.Fatalf("read session status: %v", err)
	}
	if sessionStatus != "closed" {
		t.Fatalf("console session should be closed after target delete, got %q", sessionStatus)
	}
	var historyStatus string
	if err := fixture.db.QueryRow(`
		SELECT status
		FROM history_entries
		WHERE source_ref_type = 'connector_action_request' AND source_ref_id = ?`,
		actionRequest.ID,
	).Scan(&historyStatus); err != nil {
		t.Fatalf("read stale action history: %v", err)
	}
	if historyStatus != string(connectors.ResultStale) {
		t.Fatalf("history status = %q", historyStatus)
	}
}

func TestTargetsListHidesArchivedAndMismatchedProfiles(t *testing.T) {
	fixture := newAPITestFixture(t)
	ctx := context.Background()
	store := connectortargets.NewStore(fixture.db)
	secretVault := fixture.server.activeRuntime().vault
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/targets", "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"target_id":`+strconv.FormatInt(target.ID, 10)) {
		t.Fatalf("active profile should be listed: %d %s", response.Code, response.Body.String())
	}
	if err := store.DeleteCredentialProfile(ctx, target.ID, profile.ID); err != nil {
		t.Fatalf("archive profile: %v", err)
	}
	archivedResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/targets", "", nil)
	if archivedResponse.Code != http.StatusOK {
		t.Fatalf("list targets after archive failed: %d %s", archivedResponse.Code, archivedResponse.Body.String())
	}
	if strings.Contains(archivedResponse.Body.String(), `"target_id":`+strconv.FormatInt(target.ID, 10)) {
		t.Fatalf("archived profile leaked through /api/targets: %s", archivedResponse.Body.String())
	}

	mismatchTarget, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: "postgres",
		Name:          "mismatch-db",
		Config:        map[string]any{"host": "127.0.0.1", "port": 5432, "database": "app"},
	})
	if err != nil {
		t.Fatalf("create mismatch target: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO connector_credential_profiles (
			target_id, connector_kind, kind, label, public_json, encrypted_secret_json,
			status, created_at, updated_at
		)
		VALUES (?, 'ssh', 'private_key', 'wrong-kind', '{}', 'encrypted', 'active', ?, ?)`,
		mismatchTarget.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert mismatched profile: %v", err)
	}
	mismatchResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/targets", "", nil)
	if mismatchResponse.Code != http.StatusOK {
		t.Fatalf("list targets with mismatch failed: %d %s", mismatchResponse.Code, mismatchResponse.Body.String())
	}
	if strings.Contains(mismatchResponse.Body.String(), `"target_id":`+strconv.FormatInt(mismatchTarget.ID, 10)) {
		t.Fatalf("mismatched profile leaked through /api/targets: %s", mismatchResponse.Body.String())
	}
}

func TestSSHConnectorTargetDeleteAllowsZeroProfileRollback(t *testing.T) {
	fixture := newAPITestFixture(t)
	target, err := connectortargets.NewStore(fixture.db).CreateTarget(context.Background(), connectortargets.CreateTargetInput{
		ConnectorKind: "ssh",
		Name:          "orphan-ssh",
		Config:        map[string]any{"host": "127.0.0.1", "port": 22},
	})
	if err != nil {
		t.Fatalf("create orphan ssh target: %v", err)
	}
	response := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"ok":true`) {
		t.Fatalf("zero-profile ssh target delete failed: %d %s", response.Code, response.Body.String())
	}
	if _, err := connectortargets.NewStore(fixture.db).GetTarget(context.Background(), target.ID); !errors.Is(err, connectortargets.ErrTargetNotFound) {
		t.Fatalf("zero-profile ssh target should be archived, got %v", err)
	}
}

func TestSSHConnectorTargetAllowsMultipleProfiles(t *testing.T) {
	fixture := newAPITestFixture(t)
	server := fixture.createKeyAndServer(t, "multi-profile")
	response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-targets/"+strconv.FormatInt(server.TargetID, 10)+"/profiles", "", createConnectorCredentialProfileRequest{
		Kind:  "private_key",
		Label: "backup-root",
		Public: map[string]any{
			"username":   "root",
			"ssh_key_id": server.SSHKeyID,
		},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("second SSH profile should be allowed, got %d %s", response.Code, response.Body.String())
	}
	targetResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(server.TargetID, 10), "", nil)
	if targetResponse.Code != http.StatusOK {
		t.Fatalf("get multi-profile target failed: %d %s", targetResponse.Code, targetResponse.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, targetResponse.Body.Bytes())
	if len(target.Profiles) != 2 {
		t.Fatalf("expected two profiles on SSH connector target, got %#v", target.Profiles)
	}
}

func TestSSHConnectorTargetAllowsDeletingLastProfile(t *testing.T) {
	fixture := newAPITestFixture(t)
	server := fixture.createKeyAndServer(t, "delete-profile")
	response := performJSON(fixture.server.Handler(), http.MethodDelete, "/api/connector-targets/"+strconv.FormatInt(server.TargetID, 10)+"/profiles/"+strconv.FormatInt(server.ProfileID, 10), "", nil)
	if response.Code != http.StatusNoContent {
		t.Fatalf("last SSH profile delete should be allowed, got %d %s", response.Code, response.Body.String())
	}
	targetResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-targets/"+strconv.FormatInt(server.TargetID, 10), "", nil)
	if targetResponse.Code != http.StatusOK {
		t.Fatalf("target should remain after deleting last profile: %d %s", targetResponse.Code, targetResponse.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, targetResponse.Body.Bytes())
	if len(target.Profiles) != 0 {
		t.Fatalf("expected target without profiles after profile delete, got %#v", target.Profiles)
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
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'old command', 'old', 'completed', '', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old command request: %v", err)
	}
	connectorStore := connectortargets.NewStore(fixture.db)
	connectorRequest, err := connectorStore.InsertActionRequest(context.Background(), connectortargets.InsertActionRequestInput{
		TokenID:              &token.ID,
		TargetID:             server.TargetID,
		ProfileID:            server.ProfileID,
		ConnectorKind:        "ssh",
		ActionName:           "read_console",
		Source:               "mcp",
		Input:                map[string]any{"tail_bytes": 100},
		EncryptedPayloadJSON: "encrypted",
		Reason:               "old connector action",
		Status:               connectors.ResultCompleted,
	})
	if err != nil {
		t.Fatalf("insert old connector request: %v", err)
	}
	if _, err := fixture.db.Exec(`UPDATE connector_action_requests SET created_at = ?, completed_at = ? WHERE id = ?`, old, old, connectorRequest.ID); err != nil {
		t.Fatalf("age connector request: %v", err)
	}
	if err := historypkg.NewStore(fixture.db).SyncConnectorActionRequest(context.Background(), connectorRequest.ID); err != nil {
		t.Fatalf("sync old connector history: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, token_id, runtime_id, action, payload_json, created_at)
		VALUES ('user', ?, ?, 'old.audit', '{}', ?)`,
		token.ID,
		server.ID,
		old,
	); err != nil {
		t.Fatalf("insert old audit log: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at, closed_at)
		VALUES (?, 'old console', 'closed', 'old transcript', 120, 32, ?, ?, ?)`,
		server.ID,
		old,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old console session: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO message_queue (token_id, runtime_id, direction, message, consumed_at, created_at)
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
	assertTableCount(t, fixture.db, "connector_action_requests", 0)
	assertTableCount(t, fixture.db, "history_entries", 0)
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
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, exit_code, created_at, completed_at)
		VALUES (?, ?, 'old command', 'old', 'completed', '', '', 0, ?, ?)`,
		token.ID,
		server.ID,
		old,
		old,
	); err != nil {
		t.Fatalf("insert old command request: %v", err)
	}
	if _, err := fixture.db.Exec(`
		INSERT INTO audit_logs (actor_type, token_id, runtime_id, action, payload_json, created_at)
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

func TestFileTransferRoutes(t *testing.T) {
	fixture := newAPITestFixture(t)
	server := fixture.createKeyAndServer(t, "worker-1")
	runtime := fixture.server.activeRuntime()
	tempRoot := fileTransferHandlers{fixture.server}.fileTransferTempRoot()
	if err := os.MkdirAll(tempRoot, 0o700); err != nil {
		t.Fatalf("create temp root: %v", err)
	}
	tempPath := filepath.Join(tempRoot, "download-test.txt")
	if err := os.WriteFile(tempPath, []byte("download payload"), 0o600); err != nil {
		t.Fatalf("write download file: %v", err)
	}
	record, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID:  server.ID,
		Direction:  filetransfer.DirectionDownload,
		Source:     filetransfer.SourceUI,
		RemotePath: "/var/log/app.log",
		FileName:   "app.log",
		TempPath:   tempPath,
	})
	if err != nil {
		t.Fatalf("create file transfer: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkRunning(context.Background(), record.ID); err != nil || !ok {
		t.Fatalf("mark file transfer running: ok=%v err=%v", ok, err)
	}
	if ok, err := runtime.fileTransfers.Complete(context.Background(), record.ID, int64(len("download payload")), "abc123"); err != nil || !ok {
		t.Fatalf("complete file transfer: %v", err)
	}

	listResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers?paginated=true&direction=download&q=app", "", nil)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"remote_path":"/var/log/app.log"`) {
		t.Fatalf("list file transfers failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	detailResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers/"+strconv.FormatInt(record.ID, 10), "", nil)
	if detailResponse.Code != http.StatusOK || strings.Contains(detailResponse.Body.String(), "download-test.txt") || !strings.Contains(detailResponse.Body.String(), `"checksum_sha256":"abc123"`) {
		t.Fatalf("get file transfer failed or leaked temp path: %d %s", detailResponse.Code, detailResponse.Body.String())
	}
	downloadResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers/"+strconv.FormatInt(record.ID, 10)+"/download", "", nil)
	if downloadResponse.Code != http.StatusOK || downloadResponse.Body.String() != "download payload" {
		t.Fatalf("download completed transfer failed: %d %s", downloadResponse.Code, downloadResponse.Body.String())
	}

	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfers?direction=copy", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("invalid direction should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/download", "", startDownloadRequest{RuntimeID: server.ID, RemotePath: "relative.txt"}); response.Code != http.StatusBadRequest {
		t.Fatalf("relative download path should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/browse", "", browseRemoteFilesRequest{RuntimeID: server.ID, Path: "relative"}); response.Code != http.StatusBadRequest {
		t.Fatalf("relative browse path should fail, got %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/upload", "", nil); response.Code != http.StatusBadRequest {
		t.Fatalf("missing multipart upload should fail, got %d %s", response.Code, response.Body.String())
	}

	cancelRecord, err := runtime.fileTransfers.Create(context.Background(), filetransfer.CreateRequest{
		RuntimeID:  server.ID,
		Direction:  filetransfer.DirectionUpload,
		Source:     filetransfer.SourceUI,
		LocalPath:  "movie.mp4",
		RemotePath: "/root/movie.mp4",
		FileName:   "movie.mp4",
		TempPath:   tempPath,
	})
	if err != nil {
		t.Fatalf("create cancel transfer: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkRunning(context.Background(), cancelRecord.ID); err != nil || !ok {
		t.Fatalf("mark cancel transfer running: ok=%v err=%v", ok, err)
	}
	cancelResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfers/"+strconv.FormatInt(cancelRecord.ID, 10)+"/cancel", "", map[string]any{})
	if cancelResponse.Code != http.StatusOK || !strings.Contains(cancelResponse.Body.String(), `"status":"canceled"`) {
		t.Fatalf("cancel file transfer failed: %d %s", cancelResponse.Code, cancelResponse.Body.String())
	}

	batch, err := runtime.fileTransfers.CreateBatch(context.Background(), filetransfer.CreateBatchRequest{
		RuntimeID: server.ID,
		Direction: filetransfer.DirectionUpload,
		Source:    filetransfer.SourceUI,
		Items: []filetransfer.CreateRequest{
			{LocalPath: "a.txt", RemotePath: "/tmp/a.txt", FileName: "a.txt", TempPath: tempPath},
			{LocalPath: "b.txt", RemotePath: "/tmp/b.txt", FileName: "b.txt", TempPath: tempPath},
		},
	})
	if err != nil {
		t.Fatalf("create batch: %v", err)
	}
	if ok, err := runtime.fileTransfers.MarkBatchRunning(context.Background(), batch.ID); err != nil || !ok {
		t.Fatalf("mark batch running: ok=%v err=%v", ok, err)
	}
	if ok, err := runtime.fileTransfers.PauseBatch(context.Background(), batch.ID); err != nil || !ok {
		t.Fatalf("pause batch: ok=%v err=%v", ok, err)
	}
	batchListResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/file-transfer-batches?runtime_id="+strconv.FormatInt(server.ID, 10), "", nil)
	if batchListResponse.Code != http.StatusOK {
		t.Fatalf("list file transfer batches failed: %d %s", batchListResponse.Code, batchListResponse.Body.String())
	}
	batchList := decodeRouteResponse[pageResponse[filetransfer.BatchRecord]](t, batchListResponse.Body.Bytes())
	if batchList.Total == 0 || len(batchList.Items) == 0 {
		t.Fatalf("expected batch list response to include created batch: %#v", batchList)
	}
	var listedBatch filetransfer.BatchRecord
	for _, item := range batchList.Items {
		if item.ID == batch.ID {
			listedBatch = item
			break
		}
	}
	if listedBatch.ID == 0 || len(listedBatch.Items) != 2 {
		t.Fatalf("batch list should include per-file items, got %#v", listedBatch)
	}
	duplicateQueueResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/file-transfer-batches/"+strconv.FormatInt(batch.ID, 10)+"/queue", "", map[string]any{
		"item_ids": []int64{batch.Items[0].ID, batch.Items[0].ID},
	})
	if duplicateQueueResponse.Code != http.StatusBadRequest {
		t.Fatalf("duplicate queue item ids should fail with 400, got %d %s", duplicateQueueResponse.Code, duplicateQueueResponse.Body.String())
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

func insertRouteCommandRequest(t *testing.T, database *sql.DB, tokenID int64, runtimeID int64, status string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO command_requests (token_id, runtime_id, command, reason, status, stdout, stderr, created_at)
		VALUES (?, ?, 'ls', 'test reason', ?, '', '', ?)`,
		tokenID,
		runtimeID,
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

func insertManualRouteCommandRequest(t *testing.T, database *sql.DB, runtimeID int64, command string, reason string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, tracking_reason, stdout, stderr, created_at, completed_at)
		VALUES (?, 'manual', ?, 'manual command not tracked', 'untracked', ?, '', '', ?, ?)`,
		runtimeID,
		command,
		reason,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert manual command request: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("manual command request id: %v", err)
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

	createMessageResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/messages", "", createMessageRequest{TokenID: token.ID, RuntimeID: &server.ID, Message: "hello agent"})
	if createMessageResponse.Code != http.StatusCreated {
		t.Fatalf("create message failed: %d %s", createMessageResponse.Code, createMessageResponse.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/messages?direction=user_to_ai&runtime_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello agent") {
		t.Fatalf("list messages failed: %d %s", response.Code, response.Body.String())
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at, closed_at)
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
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/sessions?runtime_id="+strconv.FormatInt(server.ID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "hello transcript") {
		t.Fatalf("list console sessions failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodGet, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10), "", nil); response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "manual") {
		t.Fatalf("get console session failed: %d %s", response.Code, response.Body.String())
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10)+"/input", "", console.InputRequest{Data: "ls\n"}); response.Code != http.StatusConflict {
		t.Fatalf("input to inactive session should conflict, got %d", response.Code)
	}
	runningResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, session_id, created_at)
		VALUES (?, 'mcp', 'sleep 60', 'test close cleanup', 'running', ?, ?)`,
		server.ID,
		sessionID,
		now,
	)
	if err != nil {
		t.Fatalf("insert running command request: %v", err)
	}
	runningRequestID, err := runningResult.LastInsertId()
	if err != nil {
		t.Fatalf("running request id: %v", err)
	}
	if response := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/sessions/"+strconv.FormatInt(sessionID, 10)+"/close", "", nil); response.Code != http.StatusOK {
		t.Fatalf("close console session failed: %d %s", response.Code, response.Body.String())
	}
	var closedRequestStatus string
	var closedRequestError string
	if err := fixture.db.QueryRow(`SELECT status, error FROM command_requests WHERE id = ?`, runningRequestID).Scan(&closedRequestStatus, &closedRequestError); err != nil {
		t.Fatalf("read closed running request: %v", err)
	}
	if closedRequestStatus != "error" || !strings.Contains(closedRequestError, "console session closed") {
		t.Fatalf("close should mark session running request error, status=%s error=%q", closedRequestStatus, closedRequestError)
	}

	restartServer := fixture.createKeyAndServer(t, "worker-restart")
	restartSessionResult, err := fixture.db.Exec(`
		INSERT INTO console_sessions (runtime_id, name, status, transcript, cols, rows, created_at, updated_at)
		VALUES (?, 'stuck', 'connected', 'stuck transcript', 120, 32, ?, ?)`,
		restartServer.ID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert restart console session: %v", err)
	}
	restartSessionID, err := restartSessionResult.LastInsertId()
	if err != nil {
		t.Fatalf("restart session id: %v", err)
	}
	restartRequestResult, err := fixture.db.Exec(`
		INSERT INTO command_requests (runtime_id, source, command, reason, status, session_id, created_at)
		VALUES (?, 'mcp', 'kubectl get nodes', 'stuck request', 'running', ?, ?)`,
		restartServer.ID,
		restartSessionID,
		now,
	)
	if err != nil {
		t.Fatalf("insert restart running command request: %v", err)
	}
	restartRequestID, err := restartRequestResult.LastInsertId()
	if err != nil {
		t.Fatalf("restart request id: %v", err)
	}
	restartResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/console/targets/"+strconv.FormatInt(restartServer.ID, 10)+"/restart", "", map[string]any{})
	if restartResponse.Code != http.StatusOK || !strings.Contains(restartResponse.Body.String(), `"status":"restarted"`) || !strings.Contains(restartResponse.Body.String(), `"target_id":`) {
		t.Fatalf("restart console session failed: %d %s", restartResponse.Code, restartResponse.Body.String())
	}
	var restartedSessionStatus string
	if err := fixture.db.QueryRow(`SELECT status FROM console_sessions WHERE id = ?`, restartSessionID).Scan(&restartedSessionStatus); err != nil {
		t.Fatalf("read restarted session: %v", err)
	}
	if restartedSessionStatus != "closed" {
		t.Fatalf("expected restarted session closed, got %s", restartedSessionStatus)
	}
	var restartedRequestStatus string
	var restartedRequestError string
	if err := fixture.db.QueryRow(`SELECT status, error FROM command_requests WHERE id = ?`, restartRequestID).Scan(&restartedRequestStatus, &restartedRequestError); err != nil {
		t.Fatalf("read restarted request: %v", err)
	}
	if restartedRequestStatus != "error" || !strings.Contains(restartedRequestError, "restarted by local user") {
		t.Fatalf("restart should mark running request error, status=%s error=%q", restartedRequestStatus, restartedRequestError)
	}

}
