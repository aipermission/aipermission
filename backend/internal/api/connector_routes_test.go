package api

import (
	"context"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors/ssh/sshkeys"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

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
	if !strings.Contains(listBody, `"kind":"postgres"`) || !strings.Contains(listBody, `"kind":"redis"`) || !strings.Contains(listBody, `"kind":"ssh"`) {
		t.Fatalf("connector list missing built-ins: %s", listBody)
	}
	if strings.Index(listBody, `"kind":"postgres"`) > strings.Index(listBody, `"kind":"redis"`) ||
		strings.Index(listBody, `"kind":"redis"`) > strings.Index(listBody, `"kind":"ssh"`) {
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
	if response := performJSON(handler, http.MethodGet, "/api/connectors/example", "", nil); response.Code != http.StatusNotFound {
		t.Fatalf("unknown connector should be not found, got %d", response.Code)
	}
}

func TestConnectorTargetHostPingRouteChecksTCPReachability(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			accepted <- conn
		}
	}()
	_, rawPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener addr: %v", err)
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse listener port: %v", err)
	}

	response := performJSON(handler, http.MethodPost, "/api/connector-targets/ping", "", connectorTargetHostPingRequest{
		Host:     "127.0.0.1",
		Port:     port,
		Mode:     "direct",
		Attempts: 2,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("ping route failed: %d %s", response.Code, response.Body.String())
	}
	page := decodeRouteResponse[connectorTargetHostPingResponse](t, response.Body.Bytes())
	if !page.OK || page.Sent != 2 || page.Received != 2 || len(page.Attempts) != 2 {
		t.Fatalf("unexpected ping response: %#v", page)
	}
	for i := 0; i < 2; i++ {
		select {
		case conn := <-accepted:
			conn.Close()
		case <-time.After(time.Second):
			t.Fatalf("listener did not receive attempt %d", i+1)
		}
	}

	bad := performJSON(handler, http.MethodPost, "/api/connector-targets/ping", "", connectorTargetHostPingRequest{
		Host: "",
		Port: port,
	})
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("missing host should be rejected, got %d %s", bad.Code, bad.Body.String())
	}

	missingProject := performJSON(handler, http.MethodPost, "/api/connector-targets/ping", "", connectorTargetHostPingRequest{
		Host:               "127.0.0.1",
		Port:               port,
		Mode:               "over_ssh",
		TransportTargetRef: "ssh:1:1",
	})
	if missingProject.Code != http.StatusBadRequest || !strings.Contains(missingProject.Body.String(), "project_id is required") {
		t.Fatalf("over-SSH ping without project should be rejected, got %d %s", missingProject.Code, missingProject.Body.String())
	}
}

func TestUnifiedTargetListIncludesSSHAndConnectorProfiles(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	sshServer := fixture.createKeyAndServer(t, "core-1")
	store := connectortargets.NewStore(fixture.db)
	pgTarget, pgProfile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault, fixture.server.activeRuntime().workspaceUUID)

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

func TestTargetsListDoesNotCreateRuntimeSurfacesOnRead(t *testing.T) {
	fixture := newAPITestFixture(t)
	handler := fixture.server.Handler()
	ctx := context.Background()
	key, err := fixture.sshKeys.Create(ctx, sshkeys.CreateRequest{Name: "lazy-key", KeyType: sshkeys.TypeED25519})
	if err != nil {
		t.Fatalf("create ssh key: %v", err)
	}
	store := connectortargets.NewStore(fixture.db)
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: "ssh",
		Name:          "lazy-ssh",
		Config:        map[string]any{"host": "127.0.0.1", "port": 22},
	})
	if err != nil {
		t.Fatalf("create ssh target: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:      target.ID,
		ConnectorKind: "ssh",
		Kind:          "private_key",
		Label:         "root",
		Public:        map[string]any{"username": "root", "ssh_key_id": key.ID, "key_name": key.Name, "key_type": key.KeyType, "fingerprint": key.Fingerprint},
	})
	if err != nil {
		t.Fatalf("create ssh profile: %v", err)
	}

	response := performJSON(handler, http.MethodGet, "/api/targets", "", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list targets failed: %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"target_id":`+strconv.FormatInt(target.ID, 10)) {
		t.Fatalf("surface-less target profile should still be listed: %s", response.Body.String())
	}
	listedPage := decodeRouteResponse[struct {
		Items []targetProfileItem `json:"items"`
	}](t, response.Body.Bytes())
	found := false
	for _, item := range listedPage.Items {
		if item.TargetID == target.ID && item.ProfileID == profile.ID {
			found = true
			if item.RuntimeID != 0 {
				t.Fatalf("list targets should not expose runtime surface for profile without one: %#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("surface-less target profile was not listed: %#v", listedPage.Items)
	}
	var count int
	if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM connector_runtime_surfaces WHERE target_id = ? AND profile_id = ?`, target.ID, profile.ID).Scan(&count); err != nil {
		t.Fatalf("count runtime surfaces: %v", err)
	}
	if count != 0 {
		t.Fatalf("list targets created %d runtime surfaces", count)
	}

	updateResponse := performJSON(handler, http.MethodPut, "/api/connector-targets/"+strconv.FormatInt(target.ID, 10), "", updateConnectorTargetRequest{
		Name:   "lazy-ssh-renamed",
		Config: map[string]any{"host": "127.0.0.1", "port": 22},
	})
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update connector target failed: %d %s", updateResponse.Code, updateResponse.Body.String())
	}
	if _, err := store.GetRuntimeSurfaceByProfile(ctx, "ssh", target.ID, profile.ID, connectortargets.RuntimeCapabilityLiveConsole); err != nil {
		t.Fatalf("target update should create runtime surface for live-console profile: %v", err)
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
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := fixture.db.Exec(`
		INSERT INTO token_connector_action_permissions (
			token_id, target_id, profile_id, action_name, execution_rule, created_at, updated_at
		) VALUES (?, ?, ?, 'removed_action', 'always_run', ?, ?)`,
		token.ID,
		target.ID,
		profile.ID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert stale connector permission: %v", err)
	}
	listWithStalePermission := performJSON(handler, http.MethodGet, "/api/tokens/"+strconv.FormatInt(token.ID, 10)+"/connector-permissions", "", nil)
	if listWithStalePermission.Code != http.StatusOK || strings.Contains(listWithStalePermission.Body.String(), "removed_action") || !strings.Contains(listWithStalePermission.Body.String(), "query_readonly") {
		t.Fatalf("stale connector permission should be filtered without hiding supported permissions: %d %s", listWithStalePermission.Code, listWithStalePermission.Body.String())
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
		ConnectorKind: "example",
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
