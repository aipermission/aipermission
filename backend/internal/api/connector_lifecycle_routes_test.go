package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectors"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	"github.com/aipermission/aipermission/backend/internal/tokens"
)

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
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault, fixture.server.activeRuntime().workspaceUUID)

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
