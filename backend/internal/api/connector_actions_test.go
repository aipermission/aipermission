package api

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/actions"
	"github.com/aipermission/aipermission/backend/internal/connectors"
	postgresconnector "github.com/aipermission/aipermission/backend/internal/connectors/postgres"
	sshconnector "github.com/aipermission/aipermission/backend/internal/connectors/ssh"
	"github.com/aipermission/aipermission/backend/internal/connectortargets"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/tokens"
	"github.com/aipermission/aipermission/backend/internal/vault"
)

func TestRuntimePrepareConnectorActionUsesLegacySSHResolver(t *testing.T) {
	database := openAPITestDB(t)
	keyID := insertAPITestSSHKey(t, database, "main")
	serverID := insertAPITestServer(t, database, keyID)
	runtime := &databaseRuntime{database: database}

	prepared, err := runtime.prepareConnectorAction(context.Background(), actions.PrepareRequest{
		Source:     "mcp",
		TargetRef:  connectortargets.SSHTargetRef(serverID),
		ActionName: sshconnector.ActionExec,
		Input:      map[string]any{"command": "uptime"},
		Reason:     "smoke",
		CreatedAt:  time.Date(2026, 6, 9, 12, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("prepare connector action: %v", err)
	}

	if prepared.Action.ConnectorKind != sshconnector.Kind {
		t.Fatalf("connector kind = %q", prepared.Action.ConnectorKind)
	}
	if prepared.Action.TargetRef != connectortargets.SSHTargetRef(serverID) {
		t.Fatalf("target ref = %q", prepared.Action.TargetRef)
	}
	if prepared.Action.ProfileID != keyID {
		t.Fatalf("profile id = %d", prepared.Action.ProfileID)
	}
	if prepared.Action.Payload["command"] != "uptime" {
		t.Fatalf("payload = %#v", prepared.Action.Payload)
	}
}

func TestCallConnectorActionBlocksMissingPermission(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := &databaseRuntime{database: database, vault: secretVault}
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)

	result, err := server.callConnectorAction(context.Background(), runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if result.Result.Status != connectors.ResultBlocked || result.Request.Status != connectors.ResultBlocked {
		t.Fatalf("expected blocked result/request, got %#v", result)
	}
	if result.Request.CompletedAt == nil || result.Request.Error == "" {
		t.Fatalf("blocked request should be terminal with error: %#v", result.Request)
	}
}

func TestCallConnectorActionCreatesPendingApproval(t *testing.T) {
	database := openAPITestDB(t)
	secretVault := openAPITestVault(t)
	runtime := &databaseRuntime{database: database, vault: secretVault}
	server := &Server{}
	store := connectortargets.NewStore(database)
	tokenID := insertAPITestToken(t, database)
	target, profile := createAPITestPostgresTargetProfile(t, store, secretVault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       tokenID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set action permission: %v", err)
	}

	result, err := server.callConnectorAction(context.Background(), runtime, connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    tokenID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1", "max_rows": 5},
		Reason:     "inspect one row",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if result.Result.Status != connectors.ResultApprovalPending || result.Request.Status != connectors.ResultApprovalPending {
		t.Fatalf("expected pending result/request, got %#v", result)
	}
	if result.Request.EncryptedPayloadJSON == "" || result.Request.ApprovalContextHash == "" {
		t.Fatalf("pending request missing encrypted payload/context: %#v", result.Request)
	}
	if result.Result.Handles.RequestID != result.Request.ID || result.Result.Handles.FollowupTool == "" {
		t.Fatalf("pending result missing followup handle: %#v", result.Result)
	}
}

func TestConnectorActionApprovalRoutesDeclinePendingRequest(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}

	listResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/connector-action-approvals?status=approval_pending", "", nil)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), strconv.FormatInt(result.Request.ID, 10)) {
		t.Fatalf("list connector approvals failed: %d %s", listResponse.Code, listResponse.Body.String())
	}
	declineResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/decline", "", declineApprovalRequest{UserNote: "not now"})
	if declineResponse.Code != http.StatusOK || !strings.Contains(declineResponse.Body.String(), `"status":"declined"`) {
		t.Fatalf("decline connector approval failed: %d %s", declineResponse.Code, declineResponse.Body.String())
	}
	mcpResponse := performJSON(fixture.server.Handler(), http.MethodGet, "/api/mcp/connector-action-requests/"+strconv.FormatInt(result.Request.ID, 10), token.TokenValue, nil)
	if mcpResponse.Code != http.StatusOK || !strings.Contains(mcpResponse.Body.String(), `"status":"declined"`) || !strings.Contains(mcpResponse.Body.String(), "not now") {
		t.Fatalf("mcp connector request should show decline: %d %s", mcpResponse.Code, mcpResponse.Body.String())
	}
}

func TestConnectorActionApprovalRunMarksDriftStale(t *testing.T) {
	fixture := newAPITestFixture(t)
	store := connectortargets.NewStore(fixture.db)
	token, err := fixture.tokens.Create(context.Background(), tokens.CreateRequest{Name: "codex"})
	if err != nil {
		t.Fatalf("create token: %v", err)
	}
	target, profile := createAPITestPostgresTargetProfile(t, store, fixture.server.activeRuntime().vault)
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionApprovalRequired,
	}); err != nil {
		t.Fatalf("set connector permission: %v", err)
	}
	result, err := fixture.server.callConnectorAction(context.Background(), fixture.server.activeRuntime(), connectorActionCall{
		Source:     commandRequestSourceMCP,
		TokenID:    token.ID,
		TargetRef:  connectortargets.ConnectorTargetRef(postgresconnector.Kind, target.ID, profile.ID),
		ActionName: postgresconnector.ActionQueryReadonly,
		Input:      map[string]any{"sql": "select 1"},
		Reason:     "smoke",
	})
	if err != nil {
		t.Fatalf("call connector action: %v", err)
	}
	if err := store.SetActionPermission(context.Background(), connectortargets.SetActionPermissionInput{
		TokenID:       token.ID,
		TargetID:      target.ID,
		ProfileID:     profile.ID,
		ActionName:    postgresconnector.ActionQueryReadonly,
		ExecutionRule: connectortargets.ActionPermissionBlocked,
	}); err != nil {
		t.Fatalf("block connector permission: %v", err)
	}

	runResponse := performJSON(fixture.server.Handler(), http.MethodPost, "/api/connector-action-approvals/"+strconv.FormatInt(result.Request.ID, 10)+"/run", "", runApprovalRequest{})
	if runResponse.Code != http.StatusConflict || !strings.Contains(runResponse.Body.String(), "fresh request") {
		t.Fatalf("expected stale conflict, got %d %s", runResponse.Code, runResponse.Body.String())
	}
	stale, err := store.GetActionRequest(context.Background(), result.Request.ID)
	if err != nil {
		t.Fatalf("get stale connector request: %v", err)
	}
	if stale.Status != connectors.ResultStale {
		t.Fatalf("status = %q", stale.Status)
	}
}

func openAPITestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbpkg.OpenEncrypted(filepath.Join(t.TempDir(), "test.db"), "test-password")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = database.Close()
	})
	return database
}

func openAPITestVault(t *testing.T) *vault.Vault {
	t.Helper()
	secretVault, err := vault.New("test-gateway-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	return secretVault
}

func createAPITestPostgresTargetProfile(t *testing.T, store *connectortargets.Store, secretVault *vault.Vault) (connectortargets.Target, connectortargets.CredentialProfile) {
	t.Helper()
	ctx := context.Background()
	target, err := store.CreateTarget(ctx, connectortargets.CreateTargetInput{
		ConnectorKind: postgresconnector.Kind,
		Name:          "main-db",
		Config: map[string]any{
			"connection_mode": "direct",
			"host":            "127.0.0.1",
			"port":            5432,
			"database":        "app",
			"ssl_mode":        "disable",
		},
	})
	if err != nil {
		t.Fatalf("create postgres target: %v", err)
	}
	encryptedSecret, err := secretVault.EncryptJSON(map[string]any{"password": "secret"})
	if err != nil {
		t.Fatalf("encrypt profile secret: %v", err)
	}
	profile, err := store.CreateCredentialProfile(ctx, connectortargets.CreateCredentialProfileInput{
		TargetID:            target.ID,
		ConnectorKind:       postgresconnector.Kind,
		Kind:                "username_password",
		Label:               "readonly",
		Public:              map[string]any{"username": "app_readonly"},
		EncryptedSecretJSON: encryptedSecret,
	})
	if err != nil {
		t.Fatalf("create postgres profile: %v", err)
	}
	return target, profile
}

func insertAPITestToken(t *testing.T, database *sql.DB) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO api_tokens (name, token_hash, token_prefix, created_at, updated_at)
		VALUES ('connector-codex', 'connector-hash', 'aip_conn', ?, ?)`,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert token: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("token id: %v", err)
	}
	return id
}

func insertAPITestSSHKey(t *testing.T, database *sql.DB, name string) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO ssh_keys (name, key_type, public_key, encrypted_private_key, fingerprint, created_at, updated_at)
		VALUES (?, 'ed25519', 'ssh-ed25519 AAAATEST aipermission-test', 'encrypted', 'SHA256:test', ?, ?)`,
		name,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert ssh key: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("ssh key id: %v", err)
	}
	return id
}

func insertAPITestServer(t *testing.T, database *sql.DB, sshKeyID int64) int64 {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := database.Exec(`
		INSERT INTO servers (name, host, port, username, ssh_key_id, description, created_at, updated_at)
		VALUES ('core-1', '10.0.0.10', 22, 'root', ?, 'test server', ?, ?)`,
		sshKeyID,
		now,
		now,
	)
	if err != nil {
		t.Fatalf("insert server: %v", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("server id: %v", err)
	}
	return id
}
