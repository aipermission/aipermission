package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/aipermission/aipermission/backend/internal/config"
	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
)

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

func TestSSHConnectorTargetWithProfileCreatesRuntimeSurface(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	ctx := context.Background()
	key, err := fixture.sshKeys.Create(ctx, sshkeys.CreateRequest{Name: "runtime-key", KeyType: sshkeys.TypeED25519})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}

	response := performJSON(handler, http.MethodPost, "/api/connector-targets/with-profile", "", createConnectorTargetWithProfileRequest{
		Target: createConnectorTargetRequest{
			ConnectorKind: "ssh",
			Name:          "runtime-ssh",
			Config:        map[string]any{"host": "127.0.0.1", "port": 22},
		},
		Profile: createConnectorCredentialProfileRequest{
			Kind:  "private_key",
			Label: "root",
			Public: map[string]any{
				"username":   "root",
				"ssh_key_id": key.ID,
			},
		},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("create ssh target with profile failed: %d %s", response.Code, response.Body.String())
	}
	target := decodeRouteResponse[connectorTargetResponse](t, response.Body.Bytes())
	if target.ID < 1 || len(target.Profiles) != 1 {
		t.Fatalf("unexpected ssh target response: %#v", target)
	}
	surface, err := connectortargets.NewStore(fixture.db).GetRuntimeSurfaceByProfile(ctx, "ssh", target.ID, target.Profiles[0].ID, connectortargets.RuntimeCapabilityLiveConsole)
	if err != nil {
		t.Fatalf("runtime surface was not created for ssh target profile: %v", err)
	}
	transferSurface, err := connectortargets.NewStore(fixture.db).GetRuntimeSurfaceByProfile(ctx, "ssh", target.ID, target.Profiles[0].ID, connectortargets.RuntimeCapabilityFileTransfer)
	if err != nil {
		t.Fatalf("file transfer surface was not created for ssh target profile: %v", err)
	}
	listResponse := performJSON(handler, http.MethodGet, "/api/targets", "", nil)
	if listResponse.Code != http.StatusOK ||
		!strings.Contains(listResponse.Body.String(), `"runtime_id":`+strconv.FormatInt(surface.ID, 10)) ||
		!strings.Contains(listResponse.Body.String(), `"transfer_runtime_id":`+strconv.FormatInt(transferSurface.ID, 10)) {
		t.Fatalf("target list should expose precreated runtime surface: %d %s", listResponse.Code, listResponse.Body.String())
	}
	if _, err := fixture.db.Exec(`DELETE FROM connector_runtime_surfaces WHERE id = ?`, transferSurface.ID); err != nil {
		t.Fatalf("delete transfer runtime surface: %v", err)
	}
	if err := fixture.server.reconcileConnectorRuntimeSurfaces(ctx, fixture.server.activeRuntime()); err != nil {
		t.Fatalf("reconcile connector runtime surfaces: %v", err)
	}
	if _, err := connectortargets.NewStore(fixture.db).GetRuntimeSurfaceByProfile(ctx, "ssh", target.ID, target.Profiles[0].ID, connectortargets.RuntimeCapabilityFileTransfer); err != nil {
		t.Fatalf("reconcile did not restore file transfer surface: %v", err)
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
